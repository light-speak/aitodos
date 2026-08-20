# ADR-0011：持久 Clarification、Continuation Run 与 Agent 配置分层

- 状态：Accepted
- 日期：2026-08-20

## 背景

实现和修订 Agent 会遇到无法安全自行决定的需求、环境或验收问题。部分 Agent CLI 会在终端中显示选项并等待输入，但让 Runner 长期保持 TTY、进程和 Lease 会占用 Worker，且 Daemon 重启后无法可靠恢复。把问题只写成普通评论又无法表达阻塞、选项、答案和续跑关系。

Agent Profile 当前把命令、参数、模型、上下文预算和检索限制平铺给用户。安全权限确实需要严格，但常规用户不应为了启用 Agent 理解所有内部预算字段。

## 决策

### 1. Clarification 是独立、可审计的领域对象

Task Agent 可以返回一个结构化阻塞问题。Clarification 保存：

```text
task_id, source_run_id, continuation_purpose
question, category, options[], recommended_option_id
allow_custom_answer, status, version
selected_option_id, custom_answer, answered_at
created_at, updated_at
```

第一阶段 category 固定为 `REQUIREMENT`、`DECISION`、`ENVIRONMENT` 或 `VALIDATION`。选项使用稳定 ID；推荐项只是说明，不自动替用户作答。问题和答案不可静默改写；回答使用乐观并发，已经回答的 Clarification 不允许重复覆盖。

普通 Agent 评论不阻塞 Task。只有显式结构化 Clarification 才进入待处理队列。CLI 权限批准和 Secret/Login 请求不属于 Clarification，不得保存到评论或 Prompt Artifact。

### 2. 提问结束当前 Run，不长期占用 Worker

Agent 返回 Clarification 后：

```text
RUNNING Run -> NEEDS_INPUT
RUNNING Task -> BLOCKED
Clarification -> OPEN
```

`NEEDS_INPUT` 是终态，Runner 退出并释放 Worker。Clarification、Run 终态和 Task 状态必须在同一短事务中提交。系统不通过 sleep、无限 Lease 或模拟通用 TTY 等待人类。

人工回答后：

```text
Clarification OPEN -> ANSWERED
Task BLOCKED -> READY 或 CHANGES_REQUESTED
Scheduler -> 新建 continuation Run
```

回答 Implementation 问题恢复 `READY`，回答 Revision 问题恢复 `CHANGES_REQUESTED`。新 Run 记录 `continuation_of_run_id`，仍遵守项目 Worker 开关、P0–P3、单 Task 活跃 Run 和 Workspace Lease。

### 3. 上下文连续性来自持久数据，不依赖 CLI Session

Continuation Run 的必需 Context 包含当前 Task、开放约束、原问题、全部选项、人工答案、来源 Run ID 和当前 Workspace 状态引用。近期评论、旧 Review 和历史 Artifact 仍按预算装入。Prompt Artifact 和 Context Manifest 记录问答来源及哈希。

Adapter 未来可以在 Capability 明确支持时 Resume 原 CLI Session，但 Session 只用于节省 Token；无法 Resume 时必须从上述持久数据重建。第一阶段不解析不稳定的原始 TTY 文本；支持结构化最终结果中的 `clarification`。未知交互式提示应失败并给出可操作错误，不能猜测答案。

### 4. 人类交互采用“就地回答 + 全局待处理”

- Task Detail 在讨论记录之前显示未回答的 Clarification 卡片。
- Header 显示全局“待回答”计数和弹窗，集中查看所有开放问题。
- 单选项一键选择，也允许 Agent 明确开放自定义回答。
- 一个“答复并继续”命令同时保存答案、恢复 Task 并进入自动调度集合。
- Answer 后卡片保留为历史，并显示来源 Run 与后续 Run 关系。

### 5. Agent 配置分为常用和高级

安全策略仍由 Role 强制，不能在 UI 降级。常用区域只显示职责 Prompt、Agent 命令和可选模型；模型保持自由填写，以兼容 CLI、Provider 和未来模型更新，并给出非强制示例。留空表示继承 Agent CLI 当前默认配置。

CLI argv 必须使用真正的多行输入，每行严格对应一个 argv，避免把空格拆分和 Shell quoting 混入系统。Codex 推荐按钮生成可直接检查的示例参数。Triage 才默认用 `--output-last-message {result_file}` 接收强制结构化结果；其他职责保留自然语言 Final Answer，机器结果需要显式写入 `ATS_RESULT_FILE`。`--approve-for-me` 与 `--sandbox` 在当前 Codex CLI 中互斥，配置校验必须提前拒绝。

近期消息数、检索结果数和超时放入折叠的“高级配置”。Token 预算不再作为普通人工配置项；以下原有硬预算决定由 [ADR-0014](0014-actual-usage-and-quality-first-soft-context-budget.md) 修订：

- `max_input_tokens - reserved_output_tokens` 只作为本地 Context Builder 的内部软目标。
- 无 Provider tokenizer 时使用中英混合保守估算：ASCII 约四字符一个 Token，非 ASCII 字符至少按一个 Token 计，避免中文内容被明显低估。
- 超限时先省略低优先级历史；安全规则、当前 Task、验收标准和 continuation 问答不得截断。
- 必需 Context 本身超限时仍完整发送，不因本地估算阻止 Run；Context Manifest 记录 `required_over_budget=true` 供诊断。
- 获取不到真实 Usage 时保持未知，不用预算值伪装成实际消耗。

## 后果

- 人工等待不占 Worker，Daemon 重启后问题和答案不丢失。
- 每次继续都会创建新 Run，执行历史和 Token 用量边界清晰。
- 不能结构化输出问题的 CLI 第一阶段无法获得通用交互式 TTY 桥接。
- Run 和 Task 状态机、Schema、Runner 结果协议、Scheduler、API 与 UI 均需要版本化更新。
- 常规 Agent 配置更简单，长任务不需要靠人工猜测 Token 预算才能执行。

## 被否决的方案

### 保持 CLI 进程等待 stdin

拒绝。它长期占用 Worker 和 Lease，Crash Recovery 难以证明进程身份，也无法兼容所有 CLI 的终端协议。

### 把问题当作普通评论

拒绝。普通评论没有选项、阻塞状态、乐观并发、来源 Run 和回答后续跑语义。

### 硬编码模型下拉列表

拒绝。模型和账户可用性变化快，不同 Provider 的 ID 也不同。UI 只提供示例和历史值，最终保存自由文本。
