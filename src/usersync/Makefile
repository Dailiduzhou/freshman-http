GO ?= go
BIN := bin/usersync

.PHONY: run
run: ## 跑一次真实调用（需要外网）
	$(GO) run ./cmd/usersync -limit 3 -log-level debug

.PHONY: run-dry
run-dry: ## 只演练，不写入上游
	$(GO) run ./cmd/usersync -limit 3 -dry-run

.PHONY: build
build: ## 编译到 bin/usersync
	$(GO) build -trimpath -o $(BIN) ./cmd/usersync

.PHONY: test
test: ## 跑全部测试（含竞态检测）
	$(GO) test -race -count=1 ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	gofmt -l -w .

.PHONY: check
check: fmt vet test ## 提交前跑一遍

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-10s %s\n", $$1, $$2}'
