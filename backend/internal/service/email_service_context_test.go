package service

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEmailServiceSendEmailReturnsPromptlyWhenContextCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	accepted := make(chan struct{})
	releaseServer := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		close(accepted)
		<-releaseServer
	}()

	t.Cleanup(func() {
		close(releaseServer)
		_ = listener.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("stalled SMTP test server did not stop")
		}
	})

	host, portString, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portString)
	require.NoError(t, err)

	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(context.Background(), map[string]string{
		SettingKeySMTPHost:     host,
		SettingKeySMTPPort:     strconv.Itoa(port),
		SettingKeySMTPUsername: "user",
		SettingKeySMTPPassword: "password",
		SettingKeySMTPFrom:     "noreply@example.com",
		SettingKeySMTPUseTLS:   "false",
	}))

	service := NewEmailService(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- service.SendEmail(ctx, "user@example.com", "subject", "body")
	}()

	select {
	case <-accepted:
	case sendErr := <-sendDone:
		t.Fatalf("SendEmail returned before the SMTP connection stalled: %v", sendErr)
	case <-time.After(time.Second):
		t.Fatal("SendEmail did not connect to the stalled SMTP server")
	}

	select {
	case sendErr := <-sendDone:
		t.Fatalf("SendEmail returned before context cancellation: %v", sendErr)
	default:
	}

	canceledAt := time.Now()
	cancel()
	select {
	case sendErr := <-sendDone:
		require.Error(t, sendErr)
		require.Less(t, time.Since(canceledAt), 500*time.Millisecond)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendEmail did not return promptly after context cancellation")
	}
}

func TestEmailServiceSendEmailReturnsPromptlyWhenTLSHandshakeCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handshakeStarted := make(chan struct{})
	serverErr := make(chan error, 1)
	releaseServer := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var clientHello [1]byte
		if _, readErr := conn.Read(clientHello[:]); readErr != nil {
			serverErr <- readErr
			return
		}
		close(handshakeStarted)
		<-releaseServer
	}()

	t.Cleanup(func() {
		close(releaseServer)
		_ = listener.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("stalled TLS test server did not stop")
		}
	})

	host, portString, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portString)
	require.NoError(t, err)
	config := &SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "user",
		Password: "password",
		From:     "noreply@example.com",
		UseTLS:   true,
	}

	service := NewEmailService(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- service.sendEmailWithConfig(ctx, config, "user@example.com", "subject", "body")
	}()

	select {
	case <-handshakeStarted:
	case serverReadErr := <-serverErr:
		t.Fatalf("TLS test server failed before handshake stalled: %v", serverReadErr)
	case sendErr := <-sendDone:
		t.Fatalf("SendEmail returned before the TLS handshake stalled: %v", sendErr)
	case <-time.After(time.Second):
		t.Fatal("SendEmail did not begin the TLS handshake")
	}

	canceledAt := time.Now()
	cancel()
	select {
	case sendErr := <-sendDone:
		require.Error(t, sendErr)
		require.Less(t, time.Since(canceledAt), 500*time.Millisecond)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendEmail did not return promptly after TLS handshake cancellation")
	}
}

func TestPrepareSMTPConnectionCleanupWaitsForCancellationCallback(t *testing.T) {
	rawConn, peerConn := net.Pipe()
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseClose) })
	}
	t.Cleanup(func() {
		release()
		_ = rawConn.Close()
		_ = peerConn.Close()
	})

	conn := &blockingCloseConn{
		Conn:         rawConn,
		closeStarted: closeStarted,
		releaseClose: releaseClose,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cleanup := prepareSMTPConnection(ctx, conn)
	cancel()

	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection cancellation callback did not start")
	}

	cleanupStarted := make(chan struct{})
	cleanupDone := make(chan struct{})
	go func() {
		close(cleanupStarted)
		cleanup()
		close(cleanupDone)
	}()
	<-cleanupStarted

	select {
	case <-cleanupDone:
		t.Fatal("SMTP connection cleanup returned while the cancellation callback was still running")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection cleanup did not return after the cancellation callback completed")
	}
}

func TestPrepareSMTPConnectionCleanupBeforeCancelPreventsCallback(t *testing.T) {
	rawConn, peerConn := net.Pipe()
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseClose) })
	}
	t.Cleanup(func() {
		release()
		_ = rawConn.Close()
		_ = peerConn.Close()
	})

	conn := &blockingCloseConn{
		Conn:         rawConn,
		closeStarted: closeStarted,
		releaseClose: releaseClose,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cleanup := prepareSMTPConnection(ctx, conn)
	cleanup()
	cancel()

	select {
	case <-closeStarted:
		t.Fatal("SMTP cancellation callback ran after cleanup unregistered it")
	case <-time.After(50 * time.Millisecond):
	}
}

type blockingCloseConn struct {
	net.Conn
	closeStarted chan struct{}
	releaseClose chan struct{}
	closeOnce    sync.Once
}

func (c *blockingCloseConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeStarted)
		<-c.releaseClose
	})
	return c.Conn.Close()
}
