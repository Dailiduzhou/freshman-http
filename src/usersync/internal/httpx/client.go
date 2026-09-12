// Package httpx 是 net/http 的一层薄封装。
//
// naive 版本里，每个 main 都在重复做同一批事：建 client、设超时、判状态码、
// 读 body、关 body、再 panic 掉所有错误。一旦要补上“重试 / 日志 / 错误分类 /
// 连接复用”，就得在每个调用点复制一遍。
//
// 这里把这些横切关注点收敛到一处：调用方只需要说清楚“方法、路径、请求体、
// 结果放哪”，其余交给 Client。它不试图重新发明一个 HTTP 框架，只负责把每个
// 真实项目里迟早都要写的那几十行写对一次。
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultMaxResponseBytes 限制我们愿意读进内存的响应体大小。
	// 不设上限的话，一个异常的 10GB 响应就能把进程打爆。
	DefaultMaxResponseBytes int64 = 4 << 20 // 4 MiB

	defaultUserAgent   = "usersync/1.0"
	defaultHTTPTimeout = 30 * time.Second
)

// RetryPolicy 描述重试策略。
type RetryPolicy struct {
	MaxAttempts int           // 总尝试次数（含第一次）；<=1 表示不重试
	BaseDelay   time.Duration // 第一次重试前等待多久
	MaxDelay    time.Duration // 退避上限
}

// DefaultRetryPolicy 是一组偏保守的默认值。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    2 * time.Second,
	}
}

// Backoff 返回第 attempt 次重试前的等待时间（attempt 从 1 开始）。
//
// 指数退避 + 抖动（jitter）。抖动很关键：没有它，一批同时失败的实例会在同一
// 时刻一起重试，把刚缓过来的上游再打垮一次（惊群）。
func (p RetryPolicy) Backoff(attempt int) time.Duration {
	if p.BaseDelay <= 0 || attempt <= 0 {
		return 0
	}
	d := p.BaseDelay
	for i := 1; i < attempt; i++ {
		if p.MaxDelay > 0 && d >= p.MaxDelay {
			break
		}
		d *= 2
	}
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	// 在 [d/2, d] 之间随机，避免固定节奏。
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// Client 是一个绑定了 baseURL 的 JSON 客户端。
// 它是并发安全的，应该被整个进程复用：每 new 一个 Client 就多一份连接池。
type Client struct {
	baseURL   string
	http      *http.Client
	log       *slog.Logger
	retry     RetryPolicy
	maxBody   int64
	userAgent string
}

// Option 用于在构造时覆盖默认行为，避免 New 长出八个参数。
type Option func(*Client)

// WithHTTPClient 注入底层 http.Client（超时、Transport、代理都由它决定）。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.http = hc
		}
	}
}

// WithLogger 注入日志器。默认丢弃所有日志（库不应该替调用方决定怎么打日志）。
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.log = l
		}
	}
}

// WithRetryPolicy 覆盖重试策略。
func WithRetryPolicy(p RetryPolicy) Option {
	return func(c *Client) {
		if p.MaxAttempts > 0 {
			c.retry = p
		}
	}
}

// WithMaxResponseBytes 覆盖响应体大小上限。
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxBody = n
		}
	}
}

// WithUserAgent 覆盖 User-Agent。真实项目里带上它，上游排查问题时能认出你。
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua = strings.TrimSpace(ua); ua != "" {
			c.userAgent = ua
		}
	}
}

// New 构造一个客户端。baseURL 必须是 http(s) 的绝对地址。
func New(baseURL string, opts ...Option) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("httpx: 解析 baseURL %q: %w", baseURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("httpx: baseURL %q 必须是 http(s) 绝对地址", baseURL)
	}

	c := &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		http:      &http.Client{Transport: NewTransport(), Timeout: defaultHTTPTimeout},
		log:       slog.New(slog.DiscardHandler),
		retry:     DefaultRetryPolicy(),
		maxBody:   DefaultMaxResponseBytes,
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Get 发起 GET，并把响应 JSON 解码到 out（out 为 nil 表示不关心响应体）。
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post 发起 POST，把 payload 序列化成 JSON 作为请求体。
//
// 注意：POST 不是幂等的，所以默认不重试——网络错误时我们无法确定服务端到底
// 有没有处理过这次请求，盲目重试可能创建出两条数据。
func (c *Client) Post(ctx context.Context, path string, payload, out any) error {
	return c.Do(ctx, http.MethodPost, path, payload, out)
}

// Do 是通用入口，method 请用 http.MethodXxx 常量。
func (c *Client) Do(ctx context.Context, method, path string, payload, out any) error {
	return c.do(ctx, method, path, payload, out)
}

func (c *Client) do(ctx context.Context, method, path string, payload, out any) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("httpx: path %q 必须以 / 开头", path)
	}
	endpoint := c.baseURL + path

	var encoded []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("httpx: 序列化请求体: %w", err)
		}
		encoded = b
	}

	policy := c.retry
	idempotent := isIdempotent(method)
	if !idempotent {
		policy.MaxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(lastErr, err)
		}

		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("httpx: 构造请求: %w", err)
		}
		c.setHeaders(req, len(encoded))

		c.log.DebugContext(ctx, "发送请求",
			slog.String("method", method),
			slog.String("url", endpoint),
			slog.Int("attempt", attempt),
		)

		resp, err := c.http.Do(req)
		if err != nil {
			// 网络层错误：连接被拒、DNS 失败、超时……都算“没拿到答案”。
			lastErr = &Error{Method: method, URL: endpoint, Retryable: idempotent, Err: err}
			if ctx.Err() != nil {
				return errors.Join(lastErr, ctx.Err())
			}
			if attempt < policy.MaxAttempts {
				delay := policy.Backoff(attempt)
				c.log.WarnContext(ctx, "请求失败，稍后重试",
					slog.String("url", endpoint),
					slog.Any("err", err),
					slog.Duration("backoff", delay),
					slog.Int("attempt", attempt),
				)
				if err := sleep(ctx, delay); err != nil {
					return errors.Join(lastErr, err)
				}
			}
			continue
		}

		body, err := readBody(resp.Body, c.maxBody) // readBody 内部负责 Close
		if err != nil {
			lastErr = &Error{Method: method, URL: endpoint, Retryable: idempotent, Err: err}
			if attempt < policy.MaxAttempts {
				if err := sleep(ctx, policy.Backoff(attempt)); err != nil {
					return errors.Join(lastErr, err)
				}
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			he := &Error{
				Method:     method,
				URL:        endpoint,
				StatusCode: resp.StatusCode,
				Retryable:  idempotent && retryableStatus(resp.StatusCode),
				Err:        errors.New(errorMessage(body)),
			}
			if !he.Retryable || attempt >= policy.MaxAttempts {
				return he
			}
			delay := policy.Backoff(attempt)
			if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
				// 上游明确告诉我们什么时候再来，就听它的。
				delay = ra
			}
			c.log.WarnContext(ctx, "上游返回可重试状态码",
				slog.String("url", endpoint),
				slog.Int("status", resp.StatusCode),
				slog.Duration("backoff", delay),
				slog.Int("attempt", attempt),
			)
			lastErr = he
			if err := sleep(ctx, delay); err != nil {
				return errors.Join(he, err)
			}
			continue
		}

		if out != nil && len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("httpx: 解析 %s %s 的响应: %w", method, endpoint, err)
			}
		}
		c.log.DebugContext(ctx, "请求成功",
			slog.Int("status", resp.StatusCode),
			slog.Duration("elapsed", time.Since(start)),
		)
		return nil
	}
	return lastErr
}

func (c *Client) setHeaders(req *http.Request, bodyLen int) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if bodyLen > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
}

// isIdempotent 判断方法是否幂等：重复执行不会产生额外副作用。
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete,
		http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// retryableStatus 只把“上游自己承认是临时故障”的状态码视为可重试。
// 4xx 是客户端的锅，重试一百次也是同样的结果。
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooEarly,            // 425
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// readBody 读完整响应体并关闭它。上限 max+1 字节：多读 1 字节就足以判断超限。
func readBody(rc io.ReadCloser, max int64) ([]byte, error) {
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应体: %w", err)
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("响应体超过 %d 字节上限", max)
	}
	return b, nil
}

// errorMessage 把错误响应的 body 压成一行短文本，方便塞进日志和错误信息里。
func errorMessage(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if s == "" {
		return "上游未返回错误详情"
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "..."
	}
	return s
}

// sleep 是可被取消的等待：ctx 一取消就立刻返回，而不是傻等退避时间结束。
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
