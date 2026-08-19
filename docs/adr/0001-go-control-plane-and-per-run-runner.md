# ADR-0001：Go Control Plane 与 Per-Run Runner

- 状态：Accepted
- 日期：2026-08-18
- Go module：`github.com/light-speak/aitodos`
- 修正：Daemon 和并发作用域由 ADR-0002 改为每项目独立
- 扩展：ADR-0003 增加不使用 Git Workspace 的 Topic Planning Run

## 背景

AiTodos 需要同时管理本地 Kanban、Task 状态机、Agent 调度、长时间运行的 CLI 进程、实时日志、取消、Crash Recovery 和 Git worktree。

Agent 执行可能持续较长时间，并可能发生卡死、日志爆量、CLI 崩溃或 Control Plane 重启。若由 Web Server 直接持有全部 CLI 子进程和管道，Server 重启会使执行跟踪、日志采集和状态恢复变得脆弱。

## 决策

采用以下技术和进程模型：

1. Control Plane 和 Runner 使用 Go 实现。
2. Web UI 使用 React + TypeScript。
3. Control Plane 与 Runner 使用同一个 Go module 和可执行文件。
4. 每个 Run 启动一个独立 Runner 进程。
5. Runner 负责 Agent 子进程组、日志、超时、取消、心跳、Git 采集和幂等 Finalization。
6. Control Plane 负责领域状态、Scheduler、并发配额、API 和恢复对账。
7. SQLite WAL 保存业务状态；日志、Diff 和大对象保存到本地 Artifact Store。
8. 一个 Task 默认拥有一个长期 Git worktree，多个 Run 顺序复用。
9. 当前 Project 的 `max_workers` 决定可用 Worker 槽位，默认值为 2；项目间互不调度。

## 原因

- Go 对进程、信号、进程组、并发和单二进制分发支持直接。
- 独立 Runner 可以隔离单次 Agent 崩溃，并允许 Control Plane 重启后重新对账。
- 每 Run 一个 Runner 的生命周期与 Run 审计边界一致。
- SQLite 和本地 Artifact Store 符合本地优先、单用户定位。
- Task 级 worktree 能隔离并发任务，并保留驳回后的连续修改上下文。

## 后果

正面影响：

- Run 有明确的进程所有者和恢复边界。
- Worker 数量可以直接表达为最大活跃 Runner 数量。
- Agent Adapter 不需要承担 Scheduler 和 Git 生命周期职责。
- 后续可以将 Runner 扩展为远程 Worker，而不重写领域模型。

代价：

- 需要设计 Runner 启动凭据、Lease、fencing token 和进程身份校验。
- 需要处理独立进程的日志 Artifact 和幂等 Finalization。
- 不同操作系统的进程组、信号和强制终止行为需要分别验证。
- SQLite 必须保持短事务，日志不能逐条写入主数据库。

## 被否决的方案

### 全 TypeScript 后端

优点是前后端共享类型更方便、初始开发速度较快。未选择的原因是本项目的核心风险集中在进程监管、信号、Crash Recovery 和本地单二进制运行，Go 更符合该执行内核。

### Agent Worker 内嵌 Control Plane

实现最简单，但 Control Plane 重启会丢失子进程管道和直接控制关系，不满足稳定性目标。

### 常驻 Worker Pool

适合远程或分布式 Worker，但 MVP 会额外引入 Worker 注册、重连、版本协商和任务转移复杂度。未来可以在不改变 Run/Lease 模型的前提下演进到该方案。

### 每 Run 一个全新 worktree

隔离更强，但会增加磁盘占用、Branch 数量、驳回续作和 Workspace 清理复杂度。MVP 选择一个 Task 一个 worktree，并通过单活跃 Run 和 Workspace Lease 保证串行修改。
