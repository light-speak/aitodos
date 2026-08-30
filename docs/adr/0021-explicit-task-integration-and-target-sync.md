# ADR-0021：显式 Task 集成与目标分支同步

- 状态：Accepted
- 日期：2026-08-30
- 影响：补齐 Task Branch 到目标分支的本地交付闭环

## 背景

人工验收会把 Task Workspace 的修改提交到长期 Task Branch，但当前不会把该 Commit 放入目标分支。Release 只读取来源分支，因此用户仍需离开 AiTodos 手工判断分支是否可合入、是否已经包含验收 Commit，以及并行 Task 是否基于过期目标分支。

把验收、集成和 Release 隐式合并成一个动作会扩大一次点击的影响，也无法安全处理并行 Task、目标分支前进、冲突和进程崩溃。

## 决策

### 1. 验收与集成保持独立

`AcceptTask` 只提交并验收 Task Workspace。新增人工显式触发的 `IntegrateTask`，系统不自动 push，也不因验收或创建 Release 自动集成。

集成必须满足：

- Task 为 `ACCEPTED`，且存在最新通过 Review 的不可变 Commit。
- Task Workspace 身份可信、干净，HEAD 与该 Review Commit 一致。
- 目标是已有 Commit 的本地 Branch。
- 当前没有活跃 Run、Workspace Lease 或未完成 Git 操作。

### 2. 目标分支只做可证明的 fast-forward

若目标分支是 Review Commit 的祖先，集成只允许 fast-forward。目标分支未被 checkout 时使用带旧 SHA 条件的原子 ref 更新；目标分支位于项目主 Working Tree 时，只有当前 Branch 正确、Working Tree 干净且没有未完成 Git 操作时，才执行显式 `git merge --ff-only`。

系统不修改不受管的其他 worktree，不自动创建非 fast-forward merge commit，也不自动 rebase 或改写 Task 历史。

目标分支已经包含 Review Commit 时，命令幂等成功；目标分支与 Task Branch 分叉时返回 `NEEDS_SYNC`，不得强行集成。

### 3. 分叉后同步 Task Workspace 并重新验证

用户可以显式执行 `SyncTarget`。系统在 Task Workspace 内尝试把当前目标分支合入 Task Branch：

- 无冲突时生成 merge Commit，并把 Task 从 `ACCEPTED` 进入 `CHANGES_REQUESTED`。
- 有冲突时立即撤销本次系统 merge，保持 Workspace 回到同步前的干净状态；记录冲突，并把 Task 进入 `CHANGES_REQUESTED`，由 Revision Agent 在受管 Workspace 中重新合并、解决冲突、运行测试。
- Revision Context 必须包含最新同步记录、目标 Branch/SHA 和冲突结果。

同步后的旧 Review 和测试证据仍保留为历史，但不得直接代表新 Workspace HEAD。Task 必须经过新的 Revision Run、测试证据和人工验收，才能再次执行 `IntegrateTask`。

### 4. 集成尝试是可恢复事实

每次改变 Git 前先保存不可变输入：

```text
task_id, review_id, operation
target_branch, source_commit_sha, target_before_sha
status, target_after_sha, workspace_after_sha
failure_kind, failure_message
created_at, updated_at
```

状态至少包括 `RUNNING`、`SUCCEEDED`、`NEEDS_SYNC`、`CONFLICT` 和 `FAILED`。同一 Task 同一时刻最多一个 `RUNNING` 集成尝试。

Control Plane 启动时对账 `RUNNING` 记录、目标 Branch、Workspace HEAD 和未完成 merge：能证明 Git 已完成时幂等补记成功；能证明没有改变时记为失败；系统拥有且仍处于冲突中的同步 merge 可以安全 abort；无法证明时隔离 Workspace，不猜测成功。

### 5. Release 只消费目标分支事实

Release 仍绑定显式选择的本地来源分支和不可变 Commit。只有通过 Git 祖先关系证明 Review Commit 已包含在 Release Commit 中时，才关联 Task。UI 优先推荐包含已集成 Task 的目标分支，不允许用 Task Branch 冒充目标分支交付状态。

### 6. 多目标分支相互独立

每个 Task 固定一个 `target_branch`。`main`、平台分支和 release 分支分别计算集成状态；系统不推断跨分支传播，也不自动 cherry-pick。同一功能需要进入多个目标分支时创建关联 Task，保留各自 Workspace、Review、测试和集成记录。

## 后果

- 人类可以在 AiTodos 内完成验收、同步、集成和 Release，同时每个高影响动作仍需显式点击。
- 并行 Task 的第二个及后续分支可能需要重新验证，这是避免用过期基线交付的必要成本。
- 第一阶段不提供自动冲突解决和自动 push；冲突交给受审计 Revision Run。
- Git 操作继续使用仓库锁、argv 调用、条件 SHA 和 Crash Recovery，不在 SQLite 事务中运行 Git。

## 被否决的方案

### 验收通过后自动 merge

拒绝。一次点击同时改变 Task 状态、创建 Commit 和修改目标分支，影响过大且难以处理并行 Task。

### 目标分支分叉时自动 rebase

拒绝。会改写已验收 Commit，破坏 Review 和 Artifact 对不可变 SHA 的引用。

### 在目标主 Working Tree 留下冲突等待人工处理

拒绝。会污染用户日常工作目录，并阻塞其他本地操作。
