# usersync —— 从 `naive-get` / `naive-post` 到工程化

`src/naive-get` 和 `src/naive-post` 用 30 行把「发一个请求」讲清楚了，这已经完成了教学任务。
但它们离「能上线的代码」还差几件事：超时之外的失败怎么办？重试几次算合理？出错时日志里能不能看出是哪个请求失败了？并发一上来会不会把上游打挂？

这个例子把那些「迟早都要补、但新手常常想不到」的部分补齐，同时**不引入任何第三方依赖**（只用标准库），
`go test ./...` 不需要联网也能跑。

## 快速开始

```bash
cd src/usersync

make run-dry          # 只演练，不写入上游
make run              # 真实调用：GET /users，再为每个用户 POST /posts
make test             # go test -race ./...
make check            # fmt + vet + test，提交前跑一遍
make help             # 列出所有目标
```

一次真实的输出（日志走 stderr，结果 JSON 走 stdout，可以放心 `| jq`）：

```
$ go run ./cmd/usersync -limit 3 -log-level debug
time=... level=INFO msg=拉取用户成功 count=3
time=... level=DEBUG msg=发送请求 method=POST url=.../posts attempt=1
time=... level=DEBUG msg=请求成功 status=201 elapsed=307ms
time=... level=INFO msg=同步完成 created=3 failed=0
{
  "base_url": "https://jsonplaceholder.typicode.com",
  "dry_run": false,
  "fetched": 3,
  "created": [
    { "id": 101, "userId": 1, "title": "欢迎 Leanne Graham 加入" }
  ],
  "failed": []
}
```

想亲眼看到重试和退出码，可以指向一个打不开的端口：

```
$ go run ./cmd/usersync -base-url http://127.0.0.1:9 -attempts 3 -log-level warn
level=WARN  msg=请求失败，稍后重试 attempt=1 backoff=125ms
level=WARN  msg=请求失败，稍后重试 attempt=2 backoff=299ms
level=ERROR msg=任务失败
$ echo $?
1
```

## 目录结构

```
src/usersync
├── cmd/usersync/          # main：只做「组装 + 决定退出码」，不含业务逻辑
│   ├── main.go
│   └── main_test.go       # 因为 run() 全是参数，可以直接测退出码和输出
├── internal/
│   ├── config/            # 参数/环境变量解析 + 校验（参数 > 环境变量 > 默认值）
│   ├── httpx/             # 通用 HTTP 客户端：超时、重试、日志、错误分类、连接池
│   │   ├── client.go
│   │   ├── errors.go
│   │   ├── transport.go
│   │   └── client_test.go
│   ├── jsonplaceholder/   # 上游接口的领域封装：路径 + 结构体 + 错误语境
│   └── app/               # 用例：读一批用户，为每人写一条动态
└── Makefile
```

分层不是为了好看，而是为了**改动的影响范围可控**：换上游只改 `jsonplaceholder`，
改重试策略只改 `httpx`，改业务规则只改 `app`。

## 和 naive 版本逐条对照

| 维度 | `naive-get` / `naive-post` | `usersync` |
| --- | --- | --- |
| 错误处理 | `panic(err)`，任何失败都崩 | 用 `%w` 包装错误链，`httpx.StatusCodeOf` 能取到状态码，`errors.Is` 能识破 `context.Canceled` |
| 超时 | `client.Timeout = 10s` | 单请求超时 + 整个任务 `deadline` + `Ctrl+C` 取消，三层 |
| 失败恢复 | 没有 | 幂等请求指数退避重试，遵守 `Retry-After`，带抖动 |
| 重试安全 | —— | **POST 默认不重试**；只重试 408/425/429/5xx |
| 日志 | `fmt.Println` | `log/slog` 结构化日志，带 `attempt`、`status`、`elapsed`、`backoff` |
| 连接复用 | 默认 Transport（每 host 空闲连接仅 2） | 调优过的 `Transport`，`MaxIdleConnsPerHost=16` |
| 响应体 | `io.ReadAll`，多大都读 | `LimitReader` 4 MiB 上限，防止被超大响应打爆内存 |
| 并发 | 串行 | 信号量限流的并发写入，保留输入顺序和逐项失败 |
| 退出码 | 无（panic → 2） | 0/1/2/3/4，和 CI、脚本、k8s 有明确契约 |
| 可测试性 | 无法测试 | 用 `httptest` 当替身上游，28 个用例覆盖超时、重试、部分失败、dry-run、配置校验 |
| 可配置性 | 硬编码 URL | 参数 + 环境变量 + 校验，配置错误立刻退出 |

## 几个「为什么」

**为什么 POST 不自动重试？**
POST 不是幂等的。网络错误发生时我们无法区分「服务端没收到」和「服务端处理完了但响应丢了」，
盲目重试可能创建出两条数据。真要允许重试，应该由服务端支持幂等键
（`Idempotency-Key`），再在 `httpx.Client` 上显式放开——见 `isIdempotent`。

**为什么要抖动（jitter）？**
所有实例同时失败，就会同时重试。固定退避会把刚缓过来的上游再压垮一次。
`RetryPolicy.Backoff` 在 `[d/2, d]` 之间随机取值来错开时间。

**为什么重试要能被打断？**
`time.Sleep` 会让收到 `SIGTERM` 的进程多等好几秒才退出，拖长发布/缩容时间。
`sleep(ctx, d)` 用 `select` 监听 `ctx.Done()`，取消时立刻返回。

**为什么日志用 stderr、结果用 stdout？**
管道下游只应该拿到结构化结果。混在一起的话，`usersync | jq` 会被日志行搞崩。

**为什么失败要分「读阶段」和「写阶段」？**
读失败 = 没有数据可用，整个任务失败并返回 error；
写失败 = 允许部分成功，把每个失败项收集进 `Result.Failed`，最后用 `errors.Join` 聚合。
这个语义属于业务用例，应该写在 `app` 里，而不是让 `main` 去猜。

## 想再往前一步，可以试这些

1. 用 `errgroup.WithContext` 替换手写的 `WaitGroup` + 信号量，体验「第一个错误就取消」的语义差异。
2. 给 `httpx.Client` 加 `Put`/`Patch`/`Delete`，注意只有 `PUT`/`DELETE` 是幂等的。
3. 加一个 `internal/middleware`，把 trace id 注入请求头，让日志能和上游日志对上。
4. 把 `Client` 抽象成接口（`type Doer interface { Do(...) }`），在 `app` 层做 mock，让用例测试完全离线。
5. 用 `httptest.NewServer` 模拟 429 + `Retry-After`，验证退避是否真的听从上游。
