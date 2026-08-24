# ADR-0017：Codex App Server、Agent Session、结构化审批与浏览器通知

- 状态：Accepted
- 日期：2026-08-21
- 影响：Agent Adapter、Run Context、权限交互和本地 UI 提醒

## 背景

一次性 `codex exec` 适合非交互任务，但无法稳定承载执行中的结构化权限请求。把 CLI 文本提示解析成按钮既脆弱，也可能在协议变化后误批准。另一方面，每次 Run 都丢弃 Agent Thread 会增加无效上下文；完全依赖 Thread 又会在 Session 失效后丢失事实。浏览器通知如果启动即请求权限或在前台重复弹出，也会干扰本地开发。

## 决策

1. 保留 `generic` 和旧 `codex` Adapter；新增推荐的 `codex-app-server` Adapter。推荐配置不要求用户维护 `exec`、沙箱和审批参数。
2. App Server 使用 stdio JSONL 协议完成 `initialize`、`thread/start` 或 `thread/resume`、`turn/start`。系统固定 Workspace、Sandbox、审批 Reviewer 和 granular approval policy，Profile Prompt 不能覆盖。
3. 命令、文件、网络和额外权限请求映射为 Schema v26 `approval_requests`。请求文本有长度上限，决定限定为仅本次、当前 Session、拒绝或停止 Run；决定通过乐观锁保存并回传同一次 Turn。
4. 未识别的结构化 Server Request 不自动回答。Run 取消、进程退出或请求消失时，开放审批进入 `CLEARED`，不在 UI 残留可点击按钮。
5. Schema v25 `agent_sessions` 保存 Topic/Task、精确 Profile Revision、Adapter、Model、外部 Thread ID 和最后 Run。兼容时原生 Resume；Resume 失败使 Session 失效，下一次人工重试创建新 Thread。
6. 每个 Run 仍完整构造并保存 Prompt Artifact 和 Context Manifest。Session 不是事实来源，也不以 Resume 推断 Token 节省。
7. 浏览器通知必须由人类点击铃铛后显式授权。开关按项目路径保存在浏览器本地；仅页面隐藏时提醒新权限、新 Clarification 和 Run 终态，并按请求 ID 或状态迁移去重。

Codex App Server 协议依据 [OpenAI 官方 App Server 文档](https://developers.openai.com/codex/app-server)。协议字段变化必须通过 Probe、固定 fixture 和容错解析验证，不得凭 UI 文案猜测。

## 后果

- 人类可以在 AiTodos 面板内理解并决定同一次 Agent Turn 的权限，不需要切回 CLI。
- Session Resume 与持久 Context 重建并存，长任务连续性不会绑定单一 Provider 隐藏状态。
- App Server 原始流继续保存为 Artifact，未知 Usage 保持未知。
- 浏览器通知是辅助入口；查询失败不会覆盖主界面的错误处理，也不会替代持久待处理列表。

## 未选择方案

### 解析交互式 CLI 文本

拒绝。自然语言提示、终端控制序列和版本变化无法提供可靠审批边界。

### 页面加载时自动请求通知权限

拒绝。缺少用户手势、容易被浏览器阻止，也会干扰本地使用。
