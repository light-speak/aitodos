# AiTodos

AiTodos 是本地优先、单用户、每项目独立运行的 Human-Agent 软件工作流控制面。

它围绕 Topic 讨论、版本化 Plan、Task 状态机、Agent Run、人工审查和 Git Workspace 组织工作，并使用 SQLite WAL、REST 与 SSE 持久化和呈现执行过程。

## 当前状态

项目处于早期可用阶段。默认不自动 push，也不自动合并到主分支；Agent 只在受管 Task Workspace 中执行。

## 本地开发

要求 Go 1.26、Node.js 22.22.2 或更高兼容版本，以及 pnpm 11.9.0。

```sh
go test ./...
go build -o ats ./cmd/ats
pnpm --dir web install --frozen-lockfile
pnpm --dir web test
pnpm --dir web build
```

运行 `./ats init` 可在当前 Git 项目中创建 `.ats/` 本地状态，`./ats start` 会以前台模式启动服务且不会自动打开浏览器。

架构与安全边界见 `docs/architecture.md` 和 `docs/adr/`。

## License

Apache License 2.0。

