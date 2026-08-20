# ADR-0008：项目级 Worker 开关、自动调度与 P0–P3

- 状态：Accepted
- 日期：2026-08-20

## 背景

正式 Task 已经表达可执行、可验收的工作。如果用户仍需逐个点击“设为可执行”、创建 Workspace 或选择 Runner，控制面就把内部编排责任转嫁给了人。另一方面，自动执行会消耗模型额度并修改代码，必须有一个清晰、项目级且可暂停的授权边界。

## 决策

1. 每个项目提供一个 `workers_enabled` 开关和一个 `max_workers` 并发数。它们属于本机项目配置，不创建跨项目全局 Scheduler。
2. `workers_enabled` 默认关闭。用户开启后，Scheduler 自动领取当前项目中所有符合条件的正式 Task；关闭只阻止新 Claim，不取消正在执行的 Run。
3. `max_workers` 默认 2，允许 1–32。修改后立即影响新 Claim，不杀死超出新上限的已有 Runner。
4. 手工创建的正式 Task 和 Plan 批准后生成的正式 Task 直接进入内部 `READY`。UI 显示“等待执行”，不提供逐 Task 的“设为可执行”按钮。
5. Topic、Clarification、Plan 草稿和 Task 草稿不是正式 Task，不得进入执行队列。`REVIEW`、`ACCEPTED`、`BLOCKED` 和 `CANCELLED` Task 也不参与普通 Implementation Claim。
6. `CHANGES_REQUESTED` Task 在 Worker 开启时自动进入 Revision 调度，不要求用户再次排队。每次 Revision 都创建新 Run，并受最大返工轮次、单 Task 单活跃 Run 和 Workspace Lease 约束。
7. Workspace 在第一次需要写代码的 Run Claim 前自动、幂等地创建或恢复。用户不管理 Workspace 生命周期；路径、Branch、HEAD 和异常只作为技术详情展示。
8. 调度排序使用稳定元组：

   ```text
   purpose class: REVISION before IMPLEMENTATION
   priority:      P0 before P1 before P2 before P3
   queued_at:     earlier first
   task_id:       stable tie breaker
   ```

   对缺少当前有效评估的 READY Task，已配置的 Triage Run 在其 Implementation Run 之前；Triage 不改变 Task 状态，失败或未配置不得永久阻塞实现。复杂度不参与此排序，详见 ADR-0010。

9. Task 优先级只允许 `P0`、`P1`、`P2`、`P3`，默认 `P2`。Label 不得隐式改变优先级。
10. Scheduler Claim 使用短事务和条件更新。事务中不得创建 Workspace、启动 Runner 或调用 Agent；外部步骤失败通过结构化 Run failure、有限重试和恢复对账处理。
11. Daemon 重启后先执行 Crash Recovery，再根据持久化开关恢复新 Claim。无法确认旧 Agent 是否执行过时标记 `LOST`，不自动重复调用 AI。
12. 单 Task 可以由用户显式暂停、取消或阻塞，但这些操作放在次级异常处理入口，不属于正常主流程。

## 备选方案

### 每个 Task 手动点击执行

拒绝。它增加重复操作，也使 Worker 并发和优先级失去意义。

### 创建 Task 后立刻创建 Workspace

拒绝。长时间排队或非代码 Task 会无谓占用 worktree。Workspace 应在写入型 Run 首次 Claim 后按需准备。

### Worker 关闭时终止所有 Runner

拒绝。关闭开关是暂停调度，不等于取消已经开始的外部进程。取消必须是独立、可审计命令。

### 使用任意整数优先级

拒绝。数值方向不直观，容易出现 `0` 与 `10` 谁更优先的歧义。固定 P0–P3 更适合人类排序和 UI 展示。

## 后果

- 人只需创建正式 Task、必要时设置优先级并进行 Review。
- Scheduler、Workspace 和 Runner 成为 Control Plane 的内部职责。
- Worker 开关是项目级 AI 执行授权，不扩大 Agent 的文件、Git 或网络权限。
- 现有 `BACKLOG → READY` 人工 Queue 入口应迁移为正式 Task 创建即 `READY`，并保留版本化 migration 和旧数据兼容处理。
