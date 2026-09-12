package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeServer(t *testing.T, postsStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":7,"name":"Zoe","username":"zoe","email":"zoe@example.com","company":{"name":"Acme"}}]`))
	})
	mux.HandleFunc("/posts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(postsStatus)
		if postsStatus < 300 {
			w.Write([]byte(`{"id":42,"userId":7,"title":"欢迎 Zoe 加入","body":"..."}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRunEndToEnd(t *testing.T) {
	srv := fakeServer(t, http.StatusCreated)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-base-url", srv.URL, "-log-level", "error", "-attempts", "1"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("退出码 = %d, 期望 %d；stderr=%s", code, exitOK, stderr.String())
	}
	var got summary
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout 应当是可解析的 JSON: %v\n%s", err, stdout.String())
	}
	if got.Fetched != 1 || len(got.Created) != 1 || got.Created[0].ID != 42 {
		t.Fatalf("汇总不对: %+v", got)
	}
	if len(got.Failed) != 0 {
		t.Fatalf("不应有失败项: %+v", got.Failed)
	}
}

func TestRunReportsUpstreamFailure(t *testing.T) {
	srv := fakeServer(t, http.StatusInternalServerError)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-base-url", srv.URL, "-log-level", "warn", "-attempts", "1"}, &stdout, &stderr)

	if code != exitUpstream {
		t.Fatalf("退出码 = %d, 期望 %d", code, exitUpstream)
	}
	if !strings.Contains(stderr.String(), "部分用户同步失败") {
		t.Fatalf("日志里应当说明部分失败: %s", stderr.String())
	}
	// 即使部分失败，机器可读的结果也必须照常输出。
	var got summary
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout 应当是可解析的 JSON: %v", err)
	}
	if len(got.Failed) != 1 {
		t.Fatalf("汇总里应当记录失败项: %+v", got)
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-attempts", "0"}, &stdout, &stderr)

	if code != exitConfig {
		t.Fatalf("退出码 = %d, 期望 %d", code, exitConfig)
	}
	if !strings.Contains(stderr.String(), "配置无效") {
		t.Fatalf("错误信息应当说明是配置问题: %s", stderr.String())
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	srv := fakeServer(t, http.StatusInternalServerError) // 一旦真的写入就会失败

	var stdout, stderr bytes.Buffer
	code := run([]string{"-base-url", srv.URL, "-dry-run", "-log-level", "error"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("dry-run 不该触发写入，退出码 = %d", code)
	}
}
