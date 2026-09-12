// Package jsonplaceholder 封装 jsonplaceholder 这个上游服务的接口。
//
// 这一层只做两件事：把路径和请求/响应结构体讲清楚，给错误补上业务语境。
// 它不关心重试、超时、日志——那些是 httpx 的职责。
// 这样分工的好处是：换一个上游，只有这个文件需要重写。
package jsonplaceholder

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"usersync/internal/httpx"
)

// 上游路径集中声明，避免字符串散落在各处。
const (
	PathUsers = "/users"
	PathPosts = "/posts"
)

// User 只声明我们会用到的字段。
// 上游返回多少字段我们管不了，但客户端应该只依赖自己真正需要的那部分契约；
// json 标签之外的字段会被自然忽略。
type User struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Username string  `json:"username"`
	Email    string  `json:"email"`
	Company  Company `json:"company"`
}

// Company 是 User 里嵌套的一个小对象。
type Company struct {
	Name string `json:"name"`
}

// Post 对应上游的一条动态。ID 用 omitempty：创建时由服务端分配，请求体里不该带。
type Post struct {
	ID     int    `json:"id,omitempty"`
	UserID int    `json:"userId"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// Client 是上游的领域客户端。
type Client struct {
	api *httpx.Client
}

// New 把通用的 HTTP 客户端包装成领域客户端。
func New(api *httpx.Client) *Client { return &Client{api: api} }

// ListUsers 拉取用户列表。limit <= 0 表示不限制。
func (c *Client) ListUsers(ctx context.Context, limit int) ([]User, error) {
	path := PathUsers
	if limit > 0 {
		path += "?" + url.Values{"_limit": {strconv.Itoa(limit)}}.Encode()
	}

	var users []User
	if err := c.api.Get(ctx, path, &users); err != nil {
		// 用 %w 包装而不是 %v：上层依然能 errors.As 出 httpx.Error 里的状态码。
		return nil, fmt.Errorf("查询用户列表: %w", err)
	}
	return users, nil
}

// CreatePost 创建一条动态。
//
// 真实项目里这种写接口通常还要传一个幂等键（Idempotency-Key）来允许安全重试，
// 这里保持简单，因此 httpx 默认不会重试 POST。
func (c *Client) CreatePost(ctx context.Context, p Post) (Post, error) {
	var created Post
	if err := c.api.Post(ctx, PathPosts, p, &created); err != nil {
		return Post{}, fmt.Errorf("创建动态(userID=%d): %w", p.UserID, err)
	}
	return created, nil
}
