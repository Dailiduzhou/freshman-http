// Command usersync 演示一个“像样”的 HTTP 客户端程序应该长什么样：
//
//	usersync 读取一批用户（GET），再为每个用户创建一条欢迎动态（POST）。
//
// 它刻意保留了 naive 版本的全部功能，但把工程上必须处理的事情补齐了：
// 超时、重试、错误分类、结构化日志、并发限流、优雅退出、可测试的边界、退出码。
//
// 日志走 stderr，结果 JSON 走 stdout，所以可以放心地 `usersync | jq`。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"usersync/internal/app"
	"usersync/internal/config"
	"usersync/internal/httpx"
	"usersync/internal/jsonplaceholder"
)

// 退出码是程序和调用方（shell 脚本、CI、k8s、监控）之间的契约，值得固定下来。
const (
	exitOK       = 0
	exitRuntime  = 1 // 意料之外的失败
	exitConfig   = 2 // 参数/配置错误
	exitUpstream = 3 // 上游返回了错误，或部分写入失败
	exitTimeout  = 4 // 整体超时
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 里没有任何 os.Exit 和全局状态，所有输入输出都是参数。
// 这让它可以被 main_test.go 直接调用，测出退出码和输出内容。
func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(args, os.Getenv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK // -h 是正常行为，不是错误
		}
		fmt.Fprintf(stderr, "usersync: %v\n", err)
		return exitConfig
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.Debug("启动", slog.Any("config", cfg))

	// Ctrl+C / SIGTERM 会取消这个 ctx，进而取消所有在飞行中的请求。
	// 这是“优雅退出”的关键：不要在收到信号后还硬着头皮把剩下的请求发完。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 再叠一层总超时兜底：即使某个循环逻辑写错，进程也不会永远挂在那里。
	ctx, cancel := context.WithTimeout(ctx, cfg.Deadline)
	defer cancel()

	// 依赖在这里组装（也就是所谓“手动 DI”）：配置决定实现，构造一次，全程复用。
	api, err := httpx.New(cfg.BaseURL,
		httpx.WithLogger(logger),
		httpx.WithUserAgent("usersync/1.0"),
		httpx.WithHTTPClient(&http.Client{
			Timeout:   cfg.Timeout,
			Transport: httpx.NewTransport(),
		}),
		httpx.WithRetryPolicy(httpx.RetryPolicy{
			MaxAttempts: cfg.Attempts,
			BaseDelay:   200 * time.Millisecond,
			MaxDelay:    2 * time.Second,
		}),
	)
	if err != nil {
		logger.Error("初始化 HTTP 客户端失败", slog.Any("err", err))
		return exitConfig
	}

	res, err := app.Run(ctx, app.Deps{
		API:     jsonplaceholder.New(api),
		Log:     logger,
		Workers: cfg.Workers,
		DryRun:  cfg.DryRun,
	}, cfg.Limit)
	if err != nil {
		logger.Error("任务失败", slog.Any("err", err))
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return exitTimeout
		case httpx.StatusCodeOf(err) != 0:
			return exitUpstream
		default:
			return exitRuntime
		}
	}

	if err := writeSummary(stdout, cfg, res); err != nil {
		logger.Error("写出结果失败", slog.Any("err", err))
		return exitRuntime
	}

	if err := res.Err(); err != nil {
		// 部分失败仍然要有明确的退出码，否则 CI 会把失败当成功。
		logger.Warn("部分用户同步失败", slog.Any("err", err))
		return exitUpstream
	}
	return exitOK
}

type createdPost struct {
	ID     int    `json:"id"`
	UserID int    `json:"userId"`
	Title  string `json:"title"`
}

type failedItem struct {
	UserID int    `json:"userId"`
	Reason string `json:"reason"`
}

// summary 是给机器读的输出结构：字段名稳定，不随日志格式变化。
type summary struct {
	BaseURL string        `json:"base_url"`
	DryRun  bool          `json:"dry_run"`
	Fetched int           `json:"fetched"`
	Created []createdPost `json:"created"`
	Failed  []failedItem  `json:"failed"`
}

func writeSummary(w io.Writer, cfg config.Config, res app.Result) error {
	s := summary{
		BaseURL: cfg.BaseURL,
		DryRun:  res.DryRun,
		Fetched: res.Fetched,
		Created: make([]createdPost, 0, len(res.Created)),
		Failed:  make([]failedItem, 0, len(res.Failed)),
	}
	for _, p := range res.Created {
		s.Created = append(s.Created, createdPost{ID: p.ID, UserID: p.UserID, Title: p.Title})
	}
	for _, f := range res.Failed {
		s.Failed = append(s.Failed, failedItem{UserID: f.UserID, Reason: f.Err.Error()})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
