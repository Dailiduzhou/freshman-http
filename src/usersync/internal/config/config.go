// Package config 负责把“外部输入”变成一份经过校验的配置。
//
// 优先级：命令行参数 > 环境变量 > 内置默认值。
// 这是命令行工具的通行约定：容器里用环境变量，人工排查时用参数临时覆盖。
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrInvalid 是所有配置错误的哨兵。main 可以用 errors.Is 把它映射成专门的退出码。
var ErrInvalid = errors.New("配置无效")

const (
	defaultBaseURL  = "https://jsonplaceholder.typicode.com"
	defaultTimeout  = 10 * time.Second
	defaultDeadline = 60 * time.Second
)

// Config 是一次运行所需要的全部参数。
type Config struct {
	BaseURL  string        // 上游服务地址
	Timeout  time.Duration // 单次 HTTP 请求超时
	Deadline time.Duration // 整个任务的超时（兜底，防止某个循环卡死）
	Attempts int           // 幂等请求的最大尝试次数
	Workers  int           // 并发创建动态的协程数
	Limit    int           // 最多处理多少个用户，0 表示全部
	DryRun   bool          // 只打印计划，不真正写上游
	LogLevel slog.Level
}

// Load 解析参数与环境变量，并做校验。
// getenv 通过参数注入，测试里就能塞一个假的，不必去动进程环境变量。
func Load(args []string, getenv func(string) string, stderr io.Writer) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if stderr == nil {
		stderr = io.Discard
	}

	timeout, err := envDuration(getenv, "USERSYNC_TIMEOUT", defaultTimeout)
	if err != nil {
		return Config{}, err
	}
	deadline, err := envDuration(getenv, "USERSYNC_DEADLINE", defaultDeadline)
	if err != nil {
		return Config{}, err
	}
	attempts, err := envInt(getenv, "USERSYNC_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	workers, err := envInt(getenv, "USERSYNC_WORKERS", 4)
	if err != nil {
		return Config{}, err
	}
	limit, err := envInt(getenv, "USERSYNC_LIMIT", 5)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		BaseURL:  envString(getenv, "USERSYNC_BASE_URL", defaultBaseURL),
		Timeout:  timeout,
		Deadline: deadline,
		Attempts: attempts,
		Workers:  workers,
		Limit:    limit,
	}
	level := envString(getenv, "USERSYNC_LOG_LEVEL", "info")

	fs := flag.NewFlagSet("usersync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "上游服务地址")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "单次 HTTP 请求超时")
	fs.DurationVar(&cfg.Deadline, "deadline", cfg.Deadline, "整个任务的超时")
	fs.IntVar(&cfg.Attempts, "attempts", cfg.Attempts, "幂等请求的最大尝试次数")
	fs.IntVar(&cfg.Workers, "workers", cfg.Workers, "并发写上游的协程数")
	fs.IntVar(&cfg.Limit, "limit", cfg.Limit, "最多处理多少个用户，0 表示全部")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "只打印将要发送的请求，不真正调用上游")
	fs.StringVar(&level, "log-level", level, "日志级别: debug|info|warn|error")

	if err := fs.Parse(args); err != nil {
		// -h/-help 会返回 flag.ErrHelp，原样往上传，交给 main 决定退出码。
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("%w: 存在多余的位置参数 %v", ErrInvalid, fs.Args())
	}

	lv, err := parseLevel(level)
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = lv

	return cfg, cfg.Validate()
}

// Validate 把所有“明显不可能跑起来”的输入挡在业务逻辑之前。
// 早失败、失败得清楚，比跑到一半 panic 要好得多。
func (c Config) Validate() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: base-url %q 不是合法的 http(s) 地址", ErrInvalid, c.BaseURL)
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: timeout 必须为正数，当前 %s", ErrInvalid, c.Timeout)
	}
	if c.Deadline <= 0 {
		return fmt.Errorf("%w: deadline 必须为正数，当前 %s", ErrInvalid, c.Deadline)
	}
	if c.Attempts < 1 {
		return fmt.Errorf("%w: attempts 至少为 1，当前 %d", ErrInvalid, c.Attempts)
	}
	if c.Workers < 1 || c.Workers > 64 {
		return fmt.Errorf("%w: workers 应在 1~64 之间，当前 %d", ErrInvalid, c.Workers)
	}
	if c.Limit < 0 {
		return fmt.Errorf("%w: limit 不能为负数，当前 %d", ErrInvalid, c.Limit)
	}
	return nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("%w: log-level %q 非法（可选 debug|info|warn|error）", ErrInvalid, s)
}

func envString(getenv func(string) string, key, def string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(getenv func(string) string, key string, def int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: 环境变量 %s=%q 不是整数: %v", ErrInvalid, key, raw, err)
	}
	return n, nil
}

func envDuration(getenv func(string) string, key string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: 环境变量 %s=%q 不是合法时长（如 5s、1m30s）: %v", ErrInvalid, key, raw, err)
	}
	return d, nil
}
