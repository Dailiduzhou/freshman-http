// Package app 是业务用例层：拉一批用户，为每个用户写一条欢迎动态。
//
// 读写两阶段的失败语义是不同的：
//   - 读阶段失败 → 拿不到数据，整个任务失败，直接返回 error；
//   - 写阶段失败 → 允许部分成功，把失败项收集起来，最后聚合汇报。
//
// 把这条规则写在用例里，而不是让 main 去猜，是“业务逻辑不依赖命令行”的体现。
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"usersync/internal/jsonplaceholder"
)

// Deps 是用例的依赖，全部由外部注入，方便替换成测试替身。
type Deps struct {
	API     *jsonplaceholder.Client
	Log     *slog.Logger
	Workers int  // 并发写上游的协程数
	DryRun  bool // 只演练不写入
}

// Failure 记录单个用户的失败，并保留原始错误链。
type Failure struct {
	UserID   int
	Username string
	Err      error
}

func (f Failure) Error() string {
	return fmt.Sprintf("用户 %d(%s): %v", f.UserID, f.Username, f.Err)
}

func (f Failure) Unwrap() error { return f.Err }

// Result 是一次同步的结果快照。
type Result struct {
	Fetched int
	Created []jsonplaceholder.Post
	Failed  []Failure
	DryRun  bool
}

// Err 把零散的失败聚合成一个错误；全部成功时返回 nil。
func (r Result) Err() error {
	if len(r.Failed) == 0 {
		return nil
	}
	errs := make([]error, 0, len(r.Failed))
	for _, f := range r.Failed {
		errs = append(errs, f)
	}
	return errors.Join(errs...)
}

// Run 执行同步任务。
func Run(ctx context.Context, deps Deps, limit int) (Result, error) {
	if deps.API == nil {
		return Result{}, errors.New("app: API 未注入")
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	workers := deps.Workers
	if workers < 1 {
		workers = 1
	}

	// 错误信息里已经有领域语境（jsonplaceholder 包补上的），这里直接用 %w 透传，
	// 不再叠加一层“获取用户列表”，免得日志里出现 “获取用户列表: 查询用户列表: ...”。
	users, err := deps.API.ListUsers(ctx, limit)
	if err != nil {
		return Result{}, err
	}
	log.InfoContext(ctx, "拉取用户成功", slog.Int("count", len(users)))

	res := Result{Fetched: len(users), DryRun: deps.DryRun}
	if len(users) == 0 {
		return res, nil
	}

	// 用带下标的结果切片收集返回值，而不是把结果通过 channel 传回来。
	// 这样输出顺序和输入顺序一致，日志和报告都更稳定、更易读。
	type outcome struct {
		post jsonplaceholder.Post
		err  error
	}
	outcomes := make([]outcome, len(users))

	// 信号量模式：限制同时在建连的协程数。
	// 直接 for 循环起 10000 个 goroutine 会瞬间打满文件描述符，也可能把上游限流。
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, u := range users {
		wg.Add(1)
		go func(i int, u jsonplaceholder.User) {
			defer wg.Done()

			sem <- struct{}{}        // 取令牌，拿不到就在这里排队
			defer func() { <-sem }() // 归还令牌

			payload := welcomePost(u)
			if deps.DryRun {
				log.InfoContext(ctx, "[dry-run] 跳过写入",
					slog.Int("userId", u.ID),
					slog.String("title", payload.Title),
				)
				outcomes[i] = outcome{post: payload}
				return
			}

			p, err := deps.API.CreatePost(ctx, payload)
			outcomes[i] = outcome{post: p, err: err}
		}(i, u)
	}
	wg.Wait()

	for i, o := range outcomes {
		if o.err != nil {
			res.Failed = append(res.Failed, Failure{
				UserID:   users[i].ID,
				Username: users[i].Username,
				Err:      o.err,
			})
			continue
		}
		res.Created = append(res.Created, o.post)
	}

	log.InfoContext(ctx, "同步完成",
		slog.Int("created", len(res.Created)),
		slog.Int("failed", len(res.Failed)),
	)
	return res, nil
}

func welcomePost(u jsonplaceholder.User) jsonplaceholder.Post {
	company := strings.TrimSpace(u.Company.Name)
	if company == "" {
		company = "（未填写公司）"
	}
	return jsonplaceholder.Post{
		UserID: u.ID,
		Title:  fmt.Sprintf("欢迎 %s 加入", u.Name),
		Body: fmt.Sprintf("你好 %s，很高兴在 %s 见到你；后续事项会发送到 %s。",
			u.Username, company, u.Email),
	}
}
