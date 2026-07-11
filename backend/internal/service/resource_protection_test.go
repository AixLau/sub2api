package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResourceProtectionDefaultMaximumRequestCharge(t *testing.T) {
	m := NewResourceProtectionManager(DefaultResourceProtectionConfig())
	first, err := m.Acquire(context.Background(), 50<<20, false)
	require.NoError(t, err)
	require.Equal(t, int64(200<<20), m.Status().ActiveBytes)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = m.Acquire(ctx, 50<<20, false)
	require.Error(t, err)

	first.Release()
	require.Eventually(t, func() bool { return m.Status().ActiveBytes == 0 }, time.Second, time.Millisecond)
}

func TestResourceProtectionRejectsDeclaredOversizeBeforeAdmission(t *testing.T) {
	m := NewResourceProtectionManager(DefaultResourceProtectionConfig())
	_, err := m.Acquire(context.Background(), (50<<20)+1, false)
	var sizeErr *RequestBodyTooLargeError
	require.ErrorAs(t, err, &sizeErr)
	require.Zero(t, m.Status().ActiveReservations)
}

func TestResourceProtectionImageConcurrencyAndLiveUpdate(t *testing.T) {
	cfg := DefaultResourceProtectionConfig()
	cfg.ImageAuditMaxConcurrency = 2
	m := NewResourceProtectionManager(cfg)

	var active atomic.Int64
	var peak atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := m.AcquireImage(context.Background())
			require.NoError(t, err)
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
			release()
		}()
	}
	close(start)
	wg.Wait()
	require.LessOrEqual(t, peak.Load(), int64(2))
	require.Zero(t, m.Status().ActiveImageAudits)

	cfg.ImageAuditMaxConcurrency = 1
	require.NoError(t, m.Update(cfg))
	ctx, cancel := context.WithCancel(context.Background())
	release, err := m.AcquireImage(ctx)
	require.NoError(t, err)
	cancel()
	_, err = m.AcquireImage(ctx)
	require.True(t, errors.Is(err, context.Canceled))
	release()
}

func TestParseGoMemoryLimit(t *testing.T) {
	bytes, ok := parseGoMemoryLimit("1500MiB")
	require.True(t, ok)
	require.Equal(t, int64(1500<<20), bytes)
	_, ok = parseGoMemoryLimit("off")
	require.False(t, ok)
}
