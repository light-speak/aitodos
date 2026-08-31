# AiTodos

<p align="center">
  <a href="https://github.com/light-speak/aitodos/actions/workflows/ci.yml"><img src="https://github.com/light-speak/aitodos/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://codecov.io/gh/light-speak/aitodos"><img src="https://codecov.io/gh/light-speak/aitodos/branch/main/graph/badge.svg" alt="Codecov" /></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go" alt="Go 1.26+" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="Apache License 2.0" /></a>
</p>

AiTodos 是本地优先、单用户、每项目独立运行的 Human-Agent 软件工作流控制面。它把长期需求讨论、可审核计划、任务执行、测试证据、人工验收和 Git Workspace 放进同一条可追踪流程。

> 项目处于早期可用阶段。默认不自动 push，也不自动合并到主分支；Agent 只在受管 Task Workspace 中执行。

## 核心流程

```text
Topic 讨论
  → Agent 澄清与版本化 Plan
  → 人工批准并创建 Task
  → 独立 Git Worktree 中执行 Agent Run
  → 测试证据与 Diff 审查
  → 人工验收、驳回或显式集成
```

主要能力：

- Topic、Plan Revision、Task 和关联讨论的持久化管理。
- Planning、Implementation、Revision 和 Review 职责分离的 Agent Run。
- 项目级 Worker 调度、优先级、取消、恢复与结构化人工问答。
- 每个 Task 长期复用独立 Git worktree，主工作区不运行 Agent。
- Run Event、SSE、日志 Artifact、Diff、测试证据和实际 Token Usage。
- 项目本地 SQLite FTS 搜索，以及受限的项目 Skill/MCP 配置。
- 明确的目标分支集成、同步状态和本地 Release Tag 记录。

## 支持平台

- macOS。
- Linux。
- Windows 通过 WSL2 使用 Linux 版本；不支持 Windows 原生 PowerShell/CMD 可执行文件。

WSL2 建议把项目放在 Linux 文件系统的 `/home/...`，避免 `/mnt/c/...` 带来的 Git worktree、文件锁和构建性能问题。Git、Agent CLI、SSH Key 和代理环境变量均应在 WSL 内配置。Windows 浏览器可以手动访问 `ats start` 输出的 `localhost` 地址。

## 环境要求

- Go 1.26 或兼容的更新版本。
- Node.js 22.22.2 或兼容的更新版本。
- pnpm 11.9.0。
- Git。
- 至少一个已配置的 Agent CLI；没有 Agent 时仍可使用项目管理和 UI 功能。

## 构建与验证

```sh
go test ./...
go vet ./...
test -z "$(gofmt -l cmd internal)"
go build -o ats ./cmd/ats

pnpm --dir web install --frozen-lockfile
pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
```

## 快速开始

在需要管理的现有 Git 仓库中运行：

```sh
/path/to/ats init
/path/to/ats start
```

`ats init` 会在当前项目创建 `.ats/`，用于保存项目配置、本地数据库、Artifact、runtime metadata 和 Task worktree。`ats start` 始终以前台模式运行，不会自动打开浏览器；终端关闭后服务随之停止。

可用命令：

```text
ats init                    初始化当前 Git 项目
ats start [--port PORT]     前台启动，不自动打开浏览器
ats status                  查看当前项目运行状态
ats open                    显式打开当前项目页面
ats stop                    停止当前项目运行进程
```

固定端口可以写入项目私有的 `.ats/local.toml`：

```toml
[server]
port = 4173
```

Linux/WSL 没有桌面环境时，`ats open` 可能不可用，直接在浏览器访问启动输出中的 URL 即可。

## Agent 与代理环境

默认代理模式为 `inherit`，Agent 继承启动 AiTodos 时的环境变量。Secret 不应写入 `.ats/local.toml`、SQLite、Issue 或日志；API Key 使用 Agent CLI 已有登录状态或环境变量引用。

```sh
export HTTP_PROXY="http://proxy.example:7890"
export HTTPS_PROXY="$HTTP_PROXY"
export ALL_PROXY="socks5://proxy.example:7890"
export NO_PROXY="localhost,127.0.0.1,::1"

./ats start
```

## 数据与安全边界

- 每个项目独立保存 `.ats/state.db`，不存在全局业务数据库或跨项目 Scheduler。
- Agent 不在项目主 Working Tree 中运行，系统自身不执行自动 push。
- Worktree 是变更隔离机制，不是操作系统安全沙箱。
- Run、Prompt、Context、Agent Profile 和策略使用不可变快照保留审计依据。
- Secret、代理凭据和 Claim Token 不进入搜索、Summary、MCP 或 Prompt Artifact。

安全问题请遵循 [`SECURITY.md`](SECURITY.md)，参与开发请阅读 [`CONTRIBUTING.md`](CONTRIBUTING.md)。

## License

[Apache License 2.0](LICENSE)
