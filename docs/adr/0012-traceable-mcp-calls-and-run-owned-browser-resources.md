# ADR-0012：可追踪 MCP 调用与 Run 所有的浏览器资源

- 状态：Accepted
- 日期：2026-08-20

## 背景

Agent 可能通过 MCP 使用浏览器、文件、数据库或其他外部工具。浏览器 page/context 如果没有明确所有者和清理阶段，Run 结束后仍可能残留进程、窗口、登录态和内存占用。只在 Prompt 中要求 Agent 自行关闭无法覆盖崩溃、超时、取消和未知 CLI 行为，也无法让用户审计 Agent 实际调用过哪些 MCP 工具。

## 决策

### 1. MCP 调用属于 Run 审计事件

结构化 Adapter 或项目受管 MCP Gateway 为每次 MCP 调用生成稳定 sequence 的 Run Event，记录：

```text
run_id, server, tool, call_id
started_at, finished_at, duration
status, error_kind
redacted_argument_summary, result_artifact_ref
```

Secret、Token、Cookie、Authorization Header 和未脱敏页面内容不得进入事件、Search、Summary 或默认 UI。大参数和结果只保存为受限 Artifact 引用。

Generic Process Adapter 无法证明调用存在或完成时保存未知能力，不从普通 stdout 文本猜测 MCP 事实。Codex Adapter 使用版本探测后的 JSONL 结构化事件；未知事件保留原始 Artifact 并容错。

### 2. 浏览器资源必须由 Run 持有

通过受管浏览器 MCP 创建的 Browser Context、Page、下载和临时 Profile 必须登记资源租约：

```text
run_id, provider, resource_kind, external_resource_id
state, created_at, cleanup_started_at, cleaned_at, cleanup_error
```

资源 ID 由 Provider 返回，不接受 Agent 自报一个无法验证的 ID。Run 只能操作和关闭自身创建的资源，不关闭用户已有窗口、其他 Run 的 context 或共享浏览器进程。

### 3. 所有终态统一清理

正常成功、失败、取消、超时和 Policy Violation 都进入同一个幂等 Cleanup 阶段。Runner 先停止新的工具调用，再按 Page、Context、临时 Profile 的逆序关闭资源，记录结果，然后完成 Finalization。

Control Plane 重启时 Recovery Manager 对账未清理租约。能够验证 Provider 和资源身份时重放清理；身份不可信时标记 `CLEANUP_REQUIRED`，提示人工处理，不猜测或关闭未知浏览器。

资源清理失败不能被静默忽略。业务结果可以保留，但 Run Detail 必须显示残留资源、失败原因和人工操作建议。

### 4. 禁止不可管理的浏览器启动方式

Agent Browser Profile 不允许调用系统 `open`、`xdg-open` 或普通 shell 启动一个无法归属和回收的浏览器。浏览器能力必须来自支持资源身份和显式 close/dispose 的 MCP Provider；不满足能力探测时禁用该能力。

AiTodos 自身的 Web UI `ats open` 与 Agent Browser Resource 是两条独立路径。前者是用户明确命令，不纳入 Run；后者必须遵守本 ADR。

## 后果

- 用户可以从 Run Detail 审计 MCP 调用，而不必阅读全部 stderr。
- Agent 终止后浏览器资源不会依赖 Prompt 自觉清理。
- 需要新增 Run Event、MCP Call 和 Run Resource 持久化，以及幂等 Cleanup/Recovery 测试。
- Generic Adapter 只能提供进程级可观测性；完整 MCP 追踪需要结构化 Adapter 或受管 Gateway。
- 共享的人工浏览器窗口不会被 Runner 误关，但不支持资源身份的浏览器 MCP 不能用于自动 Run。

## 被否决的方案

### 只在 Prompt 中要求 Agent 关闭浏览器

拒绝。进程崩溃、超时或模型遗漏时没有兜底，也无法审计是否执行。

### Run 结束时杀死所有浏览器进程

拒绝。会误伤用户窗口和其他 Run，且无法区分共享 Provider。

### 从 stdout 文本推断 MCP 调用

拒绝。格式不稳定，可能包含不可信内容，也无法可靠表达调用开始、完成和资源身份。
