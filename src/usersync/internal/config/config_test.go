package config

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	env := func(key string) string {
		if key == "USERSYNC_TIMEOUT" {
			return "3s"
		}
		return ""
	}

	// 参数 > 环境变量
	cfg, err := Load([]string{"-timeout", "5s"}, env, io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("命令行参数应当覆盖环境变量，得到 %s", cfg.Timeout)
	}

	// 环境变量 > 默认值
	cfg, err = Load(nil, env, io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Timeout != 3*time.Second {
		t.Fatalf("应当使用环境变量，得到 %s", cfg.Timeout)
	}

	// 默认值
	cfg, err = Load(nil, func(string) string { return "" }, io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != defaultBaseURL || cfg.Timeout != defaultTimeout {
		t.Fatalf("默认值不对: %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("默认日志级别应为 info，得到 %s", cfg.LogLevel)
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  func(string) string
	}{
		{"非法 base-url", []string{"-base-url", "not a url"}, nil},
		{"attempts 为 0", []string{"-attempts", "0"}, nil},
		{"workers 过大", []string{"-workers", "9999"}, nil},
		{"limit 为负", []string{"-limit", "-1"}, nil},
		{"非法 log-level", []string{"-log-level", "verbose"}, nil},
		{"多余的位置参数", []string{"extra"}, nil},
		{"环境变量不是整数", nil, func(string) string { return "abc" }},
		{"环境变量不是时长", nil, func(k string) string {
			if k == "USERSYNC_TIMEOUT" {
				return "3 seconds"
			}
			return ""
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.args, tc.env, io.Discard)
			if err == nil {
				t.Fatal("期望报错")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("错误应当能被 errors.Is(err, ErrInvalid) 命中: %v", err)
			}
		})
	}
}
