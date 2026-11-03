package repository

import (
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newConnectionLaneTestService(maxStreamsPerConnection int) *httpUpstreamService {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIHTTP2: config.GatewayOpenAIHTTP2Config{
			Enabled:                 true,
			MaxStreamsPerConnection: maxStreamsPerConnection,
		},
	}}
	return NewHTTPUpstream(cfg).(*httpUpstreamService)
}

func TestOpenAIHTTP2ConnectionLanes(t *testing.T) {
	svc := newConnectionLaneTestService(2)
	accountProxy := config.ConnectionPoolIsolationAccountProxy

	require.Equal(t, 5, svc.openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeOpenAIH2, 10))
	require.Equal(t, 2, svc.openAIHTTP2ConnectionLanes(config.ConnectionPoolIsolationAccount, upstreamProtocolModeOpenAIH2, 3))
	require.Equal(t, maxOpenAIHTTP2ConnectionLanes, svc.openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeOpenAIH2, 100))
	require.Equal(t, 1, svc.openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeOpenAIH2, 2), "并发不超过单连接容量时不拆分")
	require.Equal(t, 1, svc.openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeOpenAIH1, 10), "H1 本就一请求一连接")
	require.Equal(t, 1, svc.openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeLongStreamH2, 10))
	require.Equal(t, 1, svc.openAIHTTP2ConnectionLanes(config.ConnectionPoolIsolationProxy, upstreamProtocolModeOpenAIH2, 10), "代理级共享池不按账号拆分")
	require.Equal(t, 1, newConnectionLaneTestService(0).openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeOpenAIH2, 10), "0 表示不拆分")
	require.Equal(t, 1, (&httpUpstreamService{}).openAIHTTP2ConnectionLanes(accountProxy, upstreamProtocolModeOpenAIH2, 10))
}

// 同账号的并发请求按在途数均匀落到各分道，每个分道是独立的 Transport（独立 HTTP/2 连接）；
// 某分道请求结束后，新请求回填该分道。
func TestGetClientEntrySpreadsOpenAIHTTP2RequestsAcrossLanes(t *testing.T) {
	svc := newConnectionLaneTestService(2)
	acquire := func() *upstreamClientEntry {
		entry, err := svc.getClientEntry("", 42, 6, service.HTTPUpstreamProfileOpenAI, true, true)
		require.NoError(t, err)
		require.Equal(t, upstreamProtocolModeOpenAIH2, entry.protocolMode)
		return entry
	}

	first, second, third := acquire(), acquire(), acquire()
	require.Equal(t, []int{0, 1, 2}, []int{first.lane, second.lane, third.lane})
	require.NotSame(t, first.client.Transport, second.client.Transport)
	require.NotSame(t, second.client.Transport, third.client.Transport)

	fourth := acquire()
	require.Same(t, first, fourth, "所有分道负载相同时回到序号最小的分道")
	require.Equal(t, int64(2), atomic.LoadInt64(&first.inFlight))

	atomic.AddInt64(&second.inFlight, -1)
	require.Same(t, second, acquire(), "空出的分道优先承接新请求")
	require.Len(t, svc.clients, 3)
}

// 有在途请求却超过一个探测周期没收到数据的分道视为停顿：新请求即使负载更高也绕开它，
// 只有全部分道都停顿时才按负载回落。
func TestPickConnectionLaneSkipsStalledLane(t *testing.T) {
	svc := newConnectionLaneTestService(2)
	isolation := svc.getIsolationMode()
	settings := svc.applyProfilePoolSettings(svc.resolvePoolSettings(isolation, 4), service.HTTPUpstreamProfileOpenAI)
	poolKey := buildPoolKey(settings, upstreamProtocolModeOpenAIH2)
	baseKey := buildCacheKey(isolation, directProxyKey, 7, upstreamProtocolModeOpenAIH2)
	now := time.Now().UnixNano()
	stallAfter := 15 * time.Second
	put := func(lane int, inFlight int64, idle time.Duration) {
		entry := &upstreamClientEntry{proxyKey: directProxyKey, poolKey: poolKey, protocolMode: upstreamProtocolModeOpenAIH2, lane: lane}
		atomic.StoreInt64(&entry.inFlight, inFlight)
		atomic.StoreInt64(&entry.lastActivity, now-int64(idle))
		svc.clients[connectionLaneCacheKey(baseKey, lane)] = entry
	}

	put(0, 1, 20*time.Second)
	put(1, 2, time.Second)
	require.Equal(t, 1, svc.pickConnectionLaneLocked(baseKey, 2, isolation, directProxyKey, poolKey, now, stallAfter))

	put(1, 2, 20*time.Second)
	require.Equal(t, 0, svc.pickConnectionLaneLocked(baseKey, 2, isolation, directProxyKey, poolKey, now, stallAfter), "全部停顿时选负载最低的")

	require.Equal(t, 2, svc.pickConnectionLaneLocked(baseKey, 3, isolation, directProxyKey, poolKey, now, stallAfter), "未建立的分道视为空闲")
}

// 响应体每次读到数据都刷新分道活跃时间，并记录最长读取停顿与是否正常读到 EOF。
func TestOpenAIH2StreamBodyTracksReadGaps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		entry := &upstreamClientEntry{lane: 1}
		closed := false
		body := newOpenAIH2StreamBody(reader, entry, 9, func() { closed = true }).(*openAIH2StreamBody)
		go func() {
			_, _ = writer.Write([]byte("a"))
			time.Sleep(31 * time.Second)
			_, _ = writer.Write([]byte("b"))
			_ = writer.Close()
		}()

		data, err := io.ReadAll(body)
		require.NoError(t, err)
		require.Equal(t, "ab", string(data))
		require.True(t, body.sawEOF.Load())
		require.Equal(t, int64(31*time.Second), body.maxGap.Load())
		require.Equal(t, time.Now().UnixNano(), atomic.LoadInt64(&entry.lastActivity))

		require.NoError(t, body.Close())
		require.NoError(t, body.Close())
		require.True(t, closed)
	})
}
