# ADR-0019：Topic 自动规划轮次与 AI 方案草案

- 状态：Accepted
- 日期：2026-08-23
- 影响：补充 ADR-0003 的 Topic Planning 生命周期，不改变 Plan 人工批准边界

## 背景

Topic 的主要用途是让人类只描述问题，再由 Agent 参与讨论、澄清并形成方案。要求用户手工点击“编写方案”会让 Planner 退化为不可见配置，也无法说明 Agent 正在分析、等待补充还是执行失败。

同时，普通消息可能在 Planning Run 执行期间继续到达。系统必须避免丢失新输入、并发启动多个 Planner、旧结果覆盖新讨论和失败后的无限自动消耗。

## 决策

### 1. Topic Version 表示规划输入版本

创建 Topic 时版本为 1。追加人类消息、要求修改 Plan 或显式请求重新分析时，在保存事实和审计事件的同一事务中递增 Topic Version。Agent 回复只更新活动时间，不递增规划输入版本。

### 2. 每个 OPEN 版本最多自动尝试一次

Worker 开启且 Planner Profile 已配置时，Scheduler 自动选择尚无 PLANNING Run 记录的 OPEN Topic Version。一个 Topic 同时最多存在一个活跃 Planning Run。

如果人类在 Run 执行期间追加消息，Topic 形成新版本；当前 Run 正常收尾后，Scheduler 再为新版本创建 Continuation Run。失败、取消、超时或 LOST 都算本版本已经尝试，不自动无限重跑。用户可以通过“重新让 Agent 分析”显式创建新版本。

### 3. Planning 结果是结构化回复和可选方案

Planner 成功结果为：

```text
reply: 必填，写入 Topic 的 AGENT Message
plan: 可选，包含摘要、依据、风险、Task 草案、验收标准和测试项
```

信息不足时 Planner 只回复问题，Topic 保持 OPEN；人类发送新消息后自动开始下一轮。信息充分时 Planner 提交不可变 Plan Revision，Topic 进入 PLAN_REVIEW。Planner 不得批准 Plan、创建正式 Task 或启动 Task Run。

### 4. 旧 Run 不覆盖新输入

Planning Result 先冻结到 Run Finalization Intent，再在 Finalization 事务内幂等应用。Agent Reply 始终保留；只有 Topic 仍为当前 Run 的输入版本且状态为 OPEN 时，才应用可选 Plan Revision。如果 Topic 已有更新版本，旧 Plan 被忽略，新版本随后重新规划。

### 5. UI 以 Agent 状态为主

Topic 页面从活跃 Run 派生“已排队、Agent 分析中、等待补充、方案待确认或失败”状态，通过 Run SSE 和有界轮询刷新。手工 Plan 编辑保留为备用入口，不再作为主流程。Plan 仍必须由人类批准后，才能原子创建正式 Task。

### 6. Session 只优化连续性

同一 Topic 的兼容 Agent Session 可以 Resume，但消息、Plan、Review、Run Prompt 和 Context Manifest 才是事实来源。Resume 失败时必须从持久数据重建，不得丢失规划轮次。

## 后果

- 用户创建 Topic 或继续讨论后无需管理 Run 和 Plan 草案。
- 活跃 Run 期间的新消息不会丢失，也不会被旧方案覆盖。
- 每版本一次尝试和显式重试避免无限 Agent 循环。
- Topic Version 同时承担乐观并发与规划输入版本，需要所有人类写入保持事务一致。
- Planner 的自然语言输出必须通过结构化结果协议才能成功 Finalize。

## 被否决的方案

### 每条消息立即并发启动 Planner

响应更快，但会产生同一 Topic 的乱序回复、重复消耗和方案覆盖冲突。

### 失败后自动无限重试

可能掩盖配置错误并持续消耗用量。当前选择每版本最多一次，由人类显式重试。

### Agent 直接创建正式 Task

减少一次确认，但突破了人类审核范围、成本和验收标准的边界。当前仍要求批准 Plan。
