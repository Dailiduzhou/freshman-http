package httpx

import (
	"net/http"
	"time"
)

// NewTransport 返回一个为“长期运行 + 并发访问同一上游”调优过的 Transport。
//
// http.DefaultTransport 虽然也有连接池，但 MaxIdleConnsPerHost 只有 2。
// 并发抓取同一个 host 时，超出的连接用完就被关掉，下一轮又得重新握手，
// 白白多付出 TCP + TLS 的开销。这是 naive 版本在压测时最先暴露的问题之一。
func NewTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	t := base.Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 16
	t.IdleConnTimeout = 90 * time.Second
	t.TLSHandshakeTimeout = 10 * time.Second
	t.ExpectContinueTimeout = time.Second
	return t
}
