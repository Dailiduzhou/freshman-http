package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"usersync/internal/httpx"
)

// fastRetry 让测试里的退避时间短到几乎不可感知。
func fastRetry(attempts int) httpx.RetryPolicy {
	return httpx.RetryPolicy{
		MaxAttempts: attempts,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}
}

func TestGetRetriesTransientFailureThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c, err := httpx.New(srv.URL, httpx.WithRetryPolicy(fastRetry(3)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.Get(context.Background(), "/x", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !out.OK {
		t.Fatal("响应没有被正确解码")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("请求次数 = %d, 期望 3", got)
	}
}

func TestGetDoesNotRetryClientError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"bad request"}`)
	}))
	defer srv.Close()

	c, _ := httpx.New(srv.URL, httpx.WithRetryPolicy(fastRetry(5)))

	err := c.Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("期望拿到错误")
	}
	if got := httpx.StatusCodeOf(err); got != http.StatusBadRequest {
		t.Fatalf("StatusCodeOf = %d, 期望 400", got)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("4xx 不该重试，请求次数 = %d", got)
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("错误信息里应带上游返回的细节: %v", err)
	}
}

func TestPostIsNotRetriedByDefault(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// 策略里允许重试 5 次，但 POST 非幂等，客户端应当只尝试一次。
	c, _ := httpx.New(srv.URL, httpx.WithRetryPolicy(fastRetry(5)))

	if err := c.Post(context.Background(), "/x", map[string]string{"a": "b"}, nil); err == nil {
		t.Fatal("期望拿到错误")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("POST 不该被自动重试，请求次数 = %d", got)
	}
}

func TestRetryRespectsRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, _ := httpx.New(srv.URL, httpx.WithRetryPolicy(fastRetry(2)))

	start := time.Now()
	if err := c.Get(context.Background(), "/x", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("应当遵守 Retry-After: 1，实际只等了 %s", elapsed)
	}
}

func TestContextCancelStopsRetrying(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := httpx.New(srv.URL, httpx.WithRetryPolicy(httpx.RetryPolicy{
		MaxAttempts: 100,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	err := c.Get(ctx, "/x", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("错误应当能被 errors.Is(.., context.DeadlineExceeded) 命中，实际 %v", err)
	}
	if n := attempts.Load(); n >= 100 {
		t.Fatalf("ctx 取消后不该继续重试，请求次数 = %d", n)
	}
}

func TestClientTimeoutSurfacesAsNetError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	c, _ := httpx.New(srv.URL,
		httpx.WithHTTPClient(&http.Client{Timeout: 30 * time.Millisecond}),
		httpx.WithRetryPolicy(fastRetry(2)),
	)

	err := c.Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("期望超时错误")
	}
	var nerr net.Error
	if !errors.As(err, &nerr) || !nerr.Timeout() {
		t.Fatalf("期望一个 net.Error 超时，实际 %v", err)
	}
	if httpx.StatusCodeOf(err) != 0 {
		t.Fatal("没拿到响应，状态码应为 0")
	}
}

func TestResponseBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"padding":"this body is definitely longer than 8 bytes"}`)
	}))
	defer srv.Close()

	c, _ := httpx.New(srv.URL,
		httpx.WithMaxResponseBytes(8),
		httpx.WithRetryPolicy(fastRetry(1)),
	)

	if err := c.Get(context.Background(), "/x", nil); err == nil {
		t.Fatal("超过上限的响应体应当报错")
	}
}

func TestNewRejectsBadBaseURL(t *testing.T) {
	for _, base := range []string{"", "not-a-url", "/relative", "ftp://example.com"} {
		if _, err := httpx.New(base); err == nil {
			t.Fatalf("baseURL %q 应该被拒绝", base)
		}
	}
}

func TestBackoffIsExponentialAndJittered(t *testing.T) {
	p := httpx.RetryPolicy{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: 400 * time.Millisecond}

	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		seen[p.Backoff(1)] = true
	}
	if len(seen) < 2 {
		t.Fatal("退避应当带有抖动，不应该每次都是同一个值")
	}
	for d := range seen {
		if d < 50*time.Millisecond || d > 100*time.Millisecond {
			t.Fatalf("第 1 次退避应落在 [50ms,100ms]，实际 %s", d)
		}
	}
	if d := p.Backoff(10); d > 400*time.Millisecond {
		t.Fatalf("退避不应该超过 MaxDelay，实际 %s", d)
	}

	// 请求体的编解码也要能正常工作
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	}))
	defer srv.Close()
	c, _ := httpx.New(srv.URL)
	if err := c.Get(context.Background(), "/x", &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["hello"] != "world" {
		t.Fatalf("解码结果不对: %v", got)
	}
}
