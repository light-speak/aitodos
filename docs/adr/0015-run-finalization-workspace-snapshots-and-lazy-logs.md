# ADR-0015：Run Finalization、Workspace 快照与按需日志

- 状态：Accepted
- 日期：2026-08-21
- 影响：补充 ADR-0001、ADR-0007 和 ADR-0014 的 Runner 收尾与可观测性边界

## 背景

Agent 进程退出不等于 Run 已完整结束。无论进程成功、失败、取消或超时，Agent 都可能已经修改 Task Workspace；如果直接写入 Run 终态，数据库中的 Workspace HEAD、dirty 状态和实际文件系统会出现偏差。另一方面，stdout、stderr 和完整 Diff 可能很大，不应随 Task 详情或默认 Agent Context 自动加载。

## 决策

### 1. Agent 退出后先进入 Finalization

Runner 在 Agent 进程退出并保存可获得的日志、Usage 和结构化结果后，将 Run 从 `RUNNING` 转为 `FINALIZING`。对使用 Task Workspace 的 Run，Finalization 必须重新执行受管 Workspace 身份检查并刷新 HEAD、dirty 和状态，然后才能写入 `SUCCEEDED`、`NEEDS_INPUT`、`FAILED`、`CANCELLED` 或 `TIMED_OUT`。

结构化结果损坏、Usage 保存失败或日志索引失败等 Agent 退出后的处理错误，也必须先尽力执行同一 Workspace Finalization，再记录结构化基础设施失败。Finalization 本身失败时不得把 Workspace 当作可信状态；既有 Workspace 校验规则负责将身份异常标记为 `QUARANTINED`。

### 2. 每个使用 Workspace 的 Run 保存不可变快照

Schema v22 增加一对一 `run_workspace_snapshots`：

```text
run_id, workspace_id
branch_name, target_branch, base_commit_sha
head_before, head_after
dirty_before, dirty_after, state_after
captured_at
```

该快照用于解释某次 Run 实际接触了哪个 Workspace、开始与结束时的 Git 状态。它不替代当前 Workspace 表，也不声称 dirty 文件已经 Commit 或合入目标分支。

### 3. Run 历史与大内容分层读取

只读 API 分三层提供数据：

- `GET /api/tasks/{taskID}/runs` 只返回 Run 摘要历史。
- `GET /api/runs/{runID}` 返回结构化失败、实际 Usage、Artifact 元数据和 Workspace 快照。
- `GET /api/runs/{runID}/logs?stream=stdout|stderr` 仅在人类明确点击时读取日志正文。

日志读取只允许当前 Project Artifact Root 内由数据库索引的 stdout/stderr 文件，校验相对路径、声明大小和 SHA-256，并限制单次内联读取上限。原始日志不进入 Task 首屏、默认全文索引或默认 Agent Context。

### 4. 当前恢复边界

本 ADR 最初只落地正常 Runner 进程内的 Finalization 和可审计快照。Schema v27 和 [ADR-0018](0018-run-cancellation-finalization-and-crash-recovery.md) 已补充不可变 Finalization Intent、Runner 进程身份对账、Lease 心跳和 `FINALIZING` 崩溃重放；不明确的 Run 仍不得自动重试。

## 后果

- Agent 已修改但结果协议损坏时，修改不会被数据库静默显示为 clean。
- Task 页面能解释失败原因、Exit Code、Workspace 前后状态和实际 Usage。
- 大日志只在用户需要时读取，降低首屏开销，也避免无效占用后续 Agent Context。
- `FINALIZING` 成为真实的持久生命周期状态，为后续 Crash Recovery 提供稳定恢复点。

## 被否决的方案

### Agent 退出后直接写终态

拒绝。进程结果和 Git Workspace 是两个独立事实，缺少收尾校验会让失败、取消和超时路径丢失实际修改状态。

### Task 详情默认返回完整 stdout、stderr 和 Diff

拒绝。内容体积不可控，既影响 UI 性能，也容易被误放入搜索投影或 Agent Context。
