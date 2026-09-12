package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"usersync/internal/app"
	"usersync/internal/httpx"
	"usersync/internal/jsonplaceholder"
)

var testUsers = []jsonplaceholder.User{
	{ID: 1, Name: "Alice", Username: "alice", Email: "alice@example.com",
		Company: jsonplaceholder.Company{Name: "Acme"}},
	{ID: 2, Name: "Bob", Username: "bob", Email: "bob@example.com"},
}

// fakeUpstream 起一个替身上游：不需要联网，测试也因此是确定性的。
func fakeUpstream(t *testing.T, postsStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var created atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(testUsers)
	})
	mux.HandleFunc("/posts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if postsStatus != http.StatusOK && postsStatus != http.StatusCreated {
			w.WriteHeader(postsStatus)
			return
		}
		var in jsonplaceholder.Post
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := created.Add(1)
		in.ID = 100 + int(n)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(in)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &created
}

func newDeps(t *testing.T, baseURL string, dryRun bool) app.Deps {
	t.Helper()
	api, err := httpx.New(baseURL, httpx.WithRetryPolicy(httpx.RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}))
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	return app.Deps{API: jsonplaceholder.New(api), Workers: 2, DryRun: dryRun}
}

func TestRunCreatesOnePostPerUser(t *testing.T) {
	srv, created := fakeUpstream(t, http.StatusCreated)

	res, err := app.Run(context.Background(), newDeps(t, srv.URL, false), 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Fetched != 2 || len(res.Created) != 2 || len(res.Failed) != 0 {
		t.Fatalf("结果不对: %+v", res)
	}
	if res.Err() != nil {
		t.Fatalf("不应有聚合错误: %v", res.Err())
	}
	if got := created.Load(); got != 2 {
		t.Fatalf("上游收到 %d 次写入，期望 2", got)
	}
	// 上游的 ID 由服务端按到达顺序分配，并发下是不确定的；
	// 但我们自己收集结果时保持了输入顺序，所以这里按 UserID 断言。
	if res.Created[0].UserID != 1 || res.Created[1].UserID != 2 {
		t.Fatalf("创建结果顺序不对: %+v", res.Created)
	}
	if res.Created[0].ID == 0 || res.Created[0].ID == res.Created[1].ID {
		t.Fatalf("每个用户都应当拿到一个独立的 ID: %+v", res.Created)
	}
}

func TestRunCollectsPartialFailures(t *testing.T) {
	srv, _ := fakeUpstream(t, http.StatusInternalServerError)

	res, err := app.Run(context.Background(), newDeps(t, srv.URL, false), 0)
	// 读阶段成功，所以 Run 本身不返回错误……
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// ……但每个用户的失败都被收集起来，并在最后聚合。
	if len(res.Failed) != 2 {
		t.Fatalf("期望 2 条失败记录，实际 %+v", res.Failed)
	}
	if res.Err() == nil {
		t.Fatal("期望聚合错误")
	}
	if got := httpx.StatusCodeOf(res.Failed[0].Err); got != http.StatusInternalServerError {
		t.Fatalf("失败项应保留状态码，实际 %d", got)
	}
}

func TestRunDryRunDoesNotWrite(t *testing.T) {
	srv, created := fakeUpstream(t, http.StatusCreated)

	res, err := app.Run(context.Background(), newDeps(t, srv.URL, true), 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := created.Load(); got != 0 {
		t.Fatalf("dry-run 不该写上游，实际写了 %d 次", got)
	}
	if !res.DryRun || len(res.Created) != 2 {
		t.Fatalf("dry-run 结果不对: %+v", res)
	}
}

func TestRunFailsWhenReadPhaseFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := app.Run(context.Background(), newDeps(t, srv.URL, false), 0)
	if err == nil {
		t.Fatal("读阶段失败应当让整个任务失败")
	}
	if got := httpx.StatusCodeOf(err); got != http.StatusNotFound {
		t.Fatalf("错误链应当保留状态码，实际 %d", got)
	}
}

func TestRunRespectsCancelledContext(t *testing.T) {
	srv, _ := fakeUpstream(t, http.StatusCreated)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := app.Run(ctx, newDeps(t, srv.URL, false), 0); err == nil {
		t.Fatal("ctx 已取消时应当失败")
	}
}
