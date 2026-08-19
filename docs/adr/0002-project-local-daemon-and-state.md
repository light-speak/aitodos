# ADR-0002：每项目独立 Daemon 与本地状态

- 状态：Accepted
- 日期：2026-08-18
- 影响：修正架构基线中所有全局 Project Hub、全局 Daemon 和跨项目调度设想
- 后续修正：后台启动与内部 `_serve` 已由 ADR-0005 废止；随机端口和自动打开浏览器已由 ADR-0006 修正；每项目独立 Daemon 和本地状态决策保持有效

## 背景

AiTodos 需要表现为一个在 Git 项目内初始化和运行的命令。每个项目应能单独启动、停止、配置 Agent 和迁移数据，不依赖用户级全局数据库或常驻服务。

不同项目可能使用不同 Agent，例如 Codex、Claude 或其他可直接启动的命令，也可能使用不同模型、Proxy 和 Worker 数量。

## 决策

1. `ats init` 在当前 Git 根目录创建 `.ats` 项目状态目录。
2. Topic、Plan、Task、Run、Review、Workspace、Search Projection 等项目业务元数据保存在 `.ats/state.db`。
3. Run 日志、Diff 和 Context 保存在 `.ats/artifacts`。
4. 每个项目通过 `ats start` 启动独立 Daemon；其前台生命周期由 ADR-0005 修正。
5. 每个 Daemon 只打开当前项目数据库，只调度当前项目任务。
6. Daemon 监听 `127.0.0.1:0`，由操作系统分配随机端口。该默认行为已由 ADR-0006 扩展为可配置固定端口。
7. Daemon metadata 和 lock 保存在当前项目 `.ats/runtime`。
8. `ats stop`、`ats status` 和 `ats open` 只操作当前项目 Daemon。
9. 不创建全局项目 registry、全局 SQLite 或全局 Scheduler。
10. 每个项目在 `.ats/local.toml` 独立配置 Agent 命令、参数、模型、Proxy 和 Worker 数量，`max_workers` 默认值为 2；ADR-0006 增加本机 Server 端口配置。
11. Task worktree 保存在当前项目 `.ats/worktrees`，并被 Git 忽略。

## `.ats` 目录所有权

```text
.ats/
├── project.toml       # 可提交，不含 Secret
├── local.toml         # 忽略，本机 Agent/Proxy 配置
├── .gitignore
├── state.db           # 忽略，项目业务数据
├── state.db-wal
├── state.db-shm
├── artifacts/         # 忽略
├── runtime/           # 忽略
└── worktrees/         # 忽略
```

`.ats/project.toml` 只保存可共享的项目配置。Secret 不得写入 `project.toml`、`local.toml` 或 SQLite；只引用环境变量或 Agent 已有登录状态。

## CLI 语义

```text
ats init                 初始化当前 Git 项目
ats start                前台启动当前项目
ats start --port PORT    使用单次指定端口前台启动
ats status               显示当前项目 Daemon 状态和 URL
ats open                 打开当前项目 UI
ats stop                 优雅停止当前项目 Daemon
```

`ats start` 不再自动打开浏览器；完整端口和浏览器语义见 ADR-0006。如果当前项目 Daemon 已运行，重复执行 `ats start` 会因项目锁而失败，不启动第二个 Scheduler。

## 后果

正面影响：

- 每个项目可以独立复制、备份、删除和运行。
- 不同项目可以安全使用不同 Agent 和网络配置。
- 一个项目停止或数据库损坏不会影响其他项目。
- CLI 使用方式与 Git、开发服务器等项目级工具一致。

代价：

- 不再提供统一 Project 首页。
- 不存在跨项目全局 Worker 上限，机器总并发是各项目配置之和。
- 同时查看多个项目需要打开多个本地端口。
- 未来若增加 Project Hub，必须作为可选层，不能迁移或接管项目本地数据。

## 被否决的方案

### 全局单例 Daemon + 项目本地 SQLite

可以提供统一首页和全局并发，但引入全局 registry、生命周期耦合和跨项目调度，与“每个项目单独启动、单独退出”的目标冲突。

### 全局 SQLite

实现简单，但项目无法独立移动、备份和配置，且一个数据库故障影响所有项目。

### 强制所有项目使用同一个固定端口

多个项目同时启动时会冲突。ADR-0006 允许每个项目显式选择不同固定端口，但不提供所有项目共享的全局固定端口。
