# ADR-0020：Task 反馈意图与只读 Agent 对话

- 状态：Accepted
- 日期：2026-08-25

## 背景

Task 讨论消息已经可以持久化，但普通评论不会触发 Agent，也不会进入修订状态。人工只能在独立的 Review 区域填写驳回意见，导致“讨论”“要求修改”和 Agent Run 相互割裂。消息若产生于最后一次 Run 之后，也不会被任何后续 Agent 自动读取。

评论不能被系统猜测为写代码授权；同时，人类不应理解 Run、Profile 和状态机才能让 Agent 回答或修改。

## 决策

1. Task 输入统一提供三种显式意图：
   - `NOTE`：仅保存，不调用 Agent。
   - `DISCUSS`：保存消息并排队一个只读 `REVIEW` Run，由 Reviewer Profile 回复。
   - `REQUEST_CHANGES`：保存消息并创建可审计反馈，显式进入修订流程。
2. `REVIEW` Run 不迁移 Task 状态，不获得可写 Workspace。若 Task 已有 Workspace，只允许按 Reviewer Profile 的只读策略读取；无 Workspace 时在 Run runtime 目录执行。
3. Reviewer 最终结构化结果只允许包含 `reply`。Finalization 原子追加 Agent Task Message 并把反馈标记为已回答；失败保留失败状态，不伪造回复。
4. `REQUEST_CHANGES` 在 `READY`、`REVIEW`、`BLOCKED` 或空闲的 `CHANGES_REQUESTED` Task 上生效：
   - `REVIEW` 同时创建 `REJECTED` Review；
   - 其他允许状态创建反馈事实并进入或保持 `CHANGES_REQUESTED`；
   - `RUNNING` 时拒绝，避免消息在 Prompt 已冻结后被错误声称已采用；
   - `ACCEPTED` 时创建关联的后续修复 Task，不重写已验收历史。
5. 反馈与来源 Message、Run 和 Agent 回复建立真实外键。Claim 和反馈 `QUEUED → RUNNING` 在同一个短事务完成。
6. 只读问答优先于新的 Planning、Triage 和 Implementation，但不抢占已经运行的 Run。一个 Task 仍最多有一个活跃 Run。
7. Session Resume 只是优化。Reviewer 无法 Resume 时，从当前 Task、测试证据、Review 历史和近期讨论重建 Context。
8. 反馈处理状态使用 `QUEUED → RUNNING → ANSWERED | FAILED`；修改请求直接记录为 `APPLIED`。每次状态变化与 Task 内递增事件在同一事务提交，SSE 使用事件 `sequence` 作为 `id`，浏览器断线后可续传。
9. 失败后重试必须创建新的 Feedback 和新的 `REVIEW` Run，并分别通过 `retry_of_feedback_id`、`retry_of_run_id` 关联前一次尝试。原失败状态、错误和 Run 不得覆盖；同一次失败尝试最多直接创建一个后继，连续失败可形成有界、可审计的链。

## 交互

Task 底部只保留一个编辑器和一个意图选择：

- 默认 `询问 Agent`；
- `要求修改` 明确提示将进入修订流程；
- `仅记录` 明确提示不会调用 Agent。

Topic 讨论保持现有自动 Planning 语义，不复用 Task 反馈命令。

反馈状态直接显示在来源消息下。排队和运行状态明确提示 Agent 正在处理；失败时在原消息提供“重新询问”，回答成功后同一 Thread 出现 Agent 回复。用户不需要进入 Run 页面判断消息是否被处理。

## 后果

- 人类可以从同一输入位置完成记录、问答和修改反馈。
- 普通笔记不会消耗 Token，Agent 也不能把普通评论解释成写入授权。
- Reviewer Profile 首次成为可调度的真实职责。
- 快速完成的 Run 不再依赖前端轮询是否恰好观察到活跃状态；持久反馈事件可以重放，避免回答或失败状态丢失。
- Task Specification Revision、自动测试命令证据、依赖调度和目标分支集成仍由后续纵向切片补充；本 ADR 不用评论覆盖现有 Task 事实字段。
