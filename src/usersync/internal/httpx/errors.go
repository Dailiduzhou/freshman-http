package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// Error 是 httpx 统一对外暴露的错误类型。
//
// 有了它，调用方不必去解析错误字符串，而是可以用 errors.As / 下面的辅助函数
// 做语义判断，例如“这个失败到底是 404，还是上游 500”。
type Error struct {
	Method     string
	URL        string
	StatusCode int // 0 表示请求根本没拿到响应（网络错误、超时、取消）
	Retryable  bool
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("http %s %s: %d: %v", e.Method, e.URL, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("http %s %s: %v", e.Method, e.URL, e.Err)
}

// Unwrap 让错误链可以继续往下走：errors.Is(err, context.Canceled) 依然成立。
func (e *Error) Unwrap() error { return e.Err }

// Timeout 实现 net.Error，便于上层用 os.IsTimeout 之类的方式判断超时。
func (e *Error) Timeout() bool {
	var ne interface{ Timeout() bool }
	return errors.As(e.Err, &ne) && ne.Timeout()
}

// StatusCodeOf 从错误链里取出 HTTP 状态码，取不到返回 0。
func StatusCodeOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.StatusCode
	}
	return 0
}

// IsNotFound 之类的语义判断应该集中在包里，而不是让每个调用点写 404 字面量。
func IsNotFound(err error) bool { return StatusCodeOf(err) == http.StatusNotFound }

// IsRetryable 报告这个错误当时是否被判定为可重试。
func IsRetryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable
}
