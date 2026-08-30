# Contributing

AiTodos 使用 `main` 作为稳定分支。初始发布完成后，所有公共仓库变更都通过 Pull Request 合并。

## 开发流程

1. 从最新 `main` 创建功能分支。
2. Bug Fix 先补回归测试，新功能按 TDD 实现。
3. 运行 Go 和 Web 的完整验证。
4. 提交 Pull Request，说明行为变化、测试证据和风险。
5. 等待必需检查通过后再合并；不得绕过分支保护。

## 本地验证

```sh
go test ./...
go vet ./...
test -z "$(gofmt -l cmd internal)"
mkdir -p .tmp && go build -o .tmp/ats ./cmd/ats

pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
```

不得在 Issue、日志、测试 fixture 或提交中包含 Secret、Token、代理凭据和 SSH 私钥。
