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
- 规划充分度与诚实收口：未知项未解决时不急着提交 Plan，Run 明确区分完成、验证、未验证、风险和停止原因。
- 项目本地 SQLite FTS 搜索，以及受限的项目 Skill/MCP 配置。
- 基于真实生产搜索路径的 Recall@K、Hit@K 与 MRR 评测基线，为后续混合检索提供可比较证据。
- 经验账本与证据召回：让后续 Run 复用经过验证的项目做法，而不是反复重放全部历史对话。
- 明确的目标分支集成、同步状态和本地 Release Tag 记录。

## 为什么不是另一个 Todo List

普通 Todo List 只保存“要做什么”，AiTodos 还保存 Agent 为什么这样做、使用了哪些上下文、执行了哪些命令、产生了什么 Diff、测试证据是否可信，以及最后由谁验收。即使 Agent Session 中断，也能从项目内的结构化事实重建工作上下文。

```text
人类意图与讨论
      ↓
Topic → Plan Revision → Task → Agent Run → Review / Integration
  │                         │
  ├── Decision              ├── Prompt + Context Manifest
  └── Experience Ledger ←───┴── Test / Usage / Recall Evidence
```

### 经验账本与证据召回

AiTodos 不给一段“记忆”设置永久权重，也不把被读取次数当成正确性。经验是独立、可搜索、可追溯的记录，包含短摘要、完整做法、适用条件、来源主体和验证状态。

- `ACTIVE` 经验可以被 Run 召回；有反例时标记为 `CHALLENGED`，修订时创建新记录并把旧记录标记为 `SUPERSEDED`。
- 每次 Run 根据当前需求的文本相关性、主体范围、验证与应用结果、更新时间和人工固定状态重新评分。
- 默认最多向 Prompt 注入 5 条短摘要；完整做法通过项目只读 MCP 的 `get_experience` 按需读取。
- Run Detail 展示本次实际召回的经验和评分，人类可以标记“有帮助”“无关”或“造成误导”。
- 召回次数只用于审计和统计，不直接提高排序，避免形成自我强化偏差。
- 实现与修订 Run 可以自动提出经验候选；候选不会进入后续上下文，只有人工确认后才成为可召回经验。

测试证据同样不信任 Agent 的文字声明。只有 Runner 从可信结构化事件中观察到实际命令和匹配的退出码，才记录为命令验证；其余声明明确显示为“Agent 自报”，不计入 required 测试的验收门槛。

AiTodos 使用两个不同门槛避免 Agent 工作流失真：Planner 只有在会改变方案的未决问题已经处理后才提交 Plan 审核；Implementation/Revision 必须提供结构化收口报告。退出码为零只表示进程正常结束，不等于 Task 完成；环境、权限或本轮边界阻塞会如实记录，达到当前范围后 Agent 也应停止而不是继续扩大工作。

默认检索使用项目本地 SQLite、Unicode 文本匹配和可重建全文索引，无需额外服务即可运行。检索层将支持可选的本地或远程 Embedding，与关键词和结构化过滤组成混合召回；向量索引仍是可重建投影，不会成为项目事实的唯一来源。

搜索结果可以直接加入项目本地评测集。系统通过同一生产检索路径计算 Recall@K、Hit@K 和 MRR，并保留不可变运行历史；Embedding 只有在固定评测集上证明质量收益且满足延迟、可重建和降级要求后，才适合进入默认混合召回。

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
go mod verify
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
ats mcp                     启动当前项目只读 MCP stdio Server
ats backup [--output PATH]  备份项目事实数据
ats restore --input PATH    校验并恢复项目事实数据
ats doctor [--json]         检查数据库、外键和 Artifact 完整性
ats version                 显示版本、Commit 和构建时间
```

固定端口可以写入项目私有的 `.ats/local.toml`：

```toml
[server]
port = 4173
```

Linux/WSL 没有桌面环境时，`ats open` 可能不可用，直接在浏览器访问启动输出中的 URL 即可。

自动化脚本可以使用 `ats doctor --json` 获取带 `schema_version`、项目实例 ID、检查结论和问题列表的稳定 JSON；完整性失败时命令返回非零退出码。

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
- 项目经验只注入摘要，完整内容按需读取；每次召回、评分和人工结果都保留审计记录。

## 本地架构

- 一个 Go 可执行文件同时提供 CLI、项目 Daemon、Scheduler、Runner 和只读 MCP Server。
- React UI 由项目 Daemon 提供；REST 处理领域命令，SSE 传输实时 Run Event。
- SQLite WAL 保存规范事实和可重建 FTS 投影；大日志、Prompt、Context Manifest 与 Diff 保存为受管 Artifact。
- 每个 Run 使用独立 Runner 进程；每个 Task 长期复用一个 `.ats/worktrees` 下的 Git worktree。
- Agent Profile 按 Planning、Triage、Implementation、Revision 和 Review 职责分别配置，并在 Run 创建时冻结版本与权限快照。

## 开发

仓库使用 Go、React、TypeScript、Vite 和 pnpm。提交前至少运行：

```sh
go test ./...
go mod verify
go vet ./...
test -z "$(gofmt -l cmd internal)"
pnpm --dir web lint
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
```

安全问题请遵循 [`SECURITY.md`](SECURITY.md)，参与开发请阅读 [`CONTRIBUTING.md`](CONTRIBUTING.md)。

## License

[Apache License 2.0](LICENSE)
