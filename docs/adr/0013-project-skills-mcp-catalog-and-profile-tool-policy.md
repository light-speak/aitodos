# ADR-0013：项目 Skill/MCP 目录与 Agent Tool Policy

- 状态：Accepted
- 日期：2026-08-20

## 背景

不同项目和职责需要不同 Skill 与 MCP。把“请使用某个 MCP”写进 Prompt 不能证明能力存在，也不能阻止 Agent 调用其他已配置工具。每个 Agent 重复填写 MCP 地址和 Skill 内容又容易配置漂移、泄露 Secret，并增加 Tool Schema 与 Prompt Token。

## 决策

### 1. 项目维护能力目录

每个项目在 `.ats` 配置中维护：

- Skill Catalog：稳定 ID、显示名、来源引用、版本或内容哈希、启用状态和 Probe 结果。
- MCP Server Catalog：稳定 ID、本机 Codex 配置名引用、启用状态和 Probe 结果。MVP 不复制传输配置；后续若由 AiTodos 管理 Provider，再只保存环境变量名等脱敏引用。

Secret 只使用环境变量名或外部 Secret 引用，SQLite 和 Run Snapshot 不保存明文。全局已安装能力可以被项目引用，但不会自动对所有项目或 Agent 开放。

### 2. Agent Profile Revision 保存 Tool Policy

Profile Revision 保存：

```text
allowed_skill_ids
allowed_mcp_server_ids
allowed_tool_patterns
required_capability_ids
```

默认拒绝未列出的能力。项目目录负责“一次配置”，Profile 负责“哪个职责能用”。权限策略仍由系统 Role 上限约束；Profile 不能让 Planner 获得写 Workspace，也不能让 Reviewer 获得高影响写 Tool。

### 3. Run 固化能力快照

Run 创建时解析 Profile Revision 与当前项目目录，固化 Skill 来源/哈希、MCP Server 配置的脱敏快照、Tool allowlist 和 Probe 结果。后续修改项目目录只影响未来 Run。

required 能力不可用时，在模型调用前失败；optional 能力不可用时记录明确降级。Message、Task、搜索结果和 Agent 输出均不能新增 Skill、Server 或 Tool 权限。

### 4. Token 与加载策略

只向当前 Run 暴露允许且与职责相关的 MCP Tool Schema。MVP 将 Profile 明确选择的 Skill 交给 Context Builder，不注入项目其他 Skill；optional Skill 可因失效或预算不足省略，required Skill 不可省略。后续支持 Skill 触发规则时，不得扩大 Profile 的允许集合。Context Manifest 记录实际采用的 Skill、哈希、Token 估算和省略原因。

### 5. UI

“项目能力”页面集中配置和 Probe Skill/MCP。Agent 配置页面只选择允许的能力，并展示系统强制权限、required/optional 状态和预计 Context 影响。Task 和评论不能临时开启能力。

MCP 调用审计和浏览器资源回收遵循 ADR-0012。

Schema v19 和首个运行时纵向切片已经落地能力目录、Skill 重新校验、Profile Revision 绑定、Run 快照、Codex Server 默认拒绝和 Tool allowlist。MCP 调用事件、浏览器资源租约与幂等清理仍属于 ADR-0012 的后续实现，UI 不得把“已限制能力”显示成“已完整追踪调用”。

## 后果

- 用户可以为项目指定自己的 Skill 和 MCP，而不用重复配置每个 Agent。
- 职责隔离由运行时配置执行，不依赖 Prompt 自律。
- 少加载无关 Tool Schema 和 Skill 正文可以降低 Context 用量。
- 需要能力 Probe、稳定引用、Run Snapshot 和配置冲突 UI。

## 被否决的方案

### 所有 Agent 自动继承全部全局 Skill/MCP

拒绝。权限面和 Token 成本不可控，项目也无法复现一次 Run 的能力集合。

### 只在 Prompt 中指定工具

拒绝。Prompt 不是权限边界，且不能处理 Server 不可用、Secret 引用和调用审计。

### 每个 Task 自由填写 MCP 地址

拒绝。容易泄露 Secret、造成配置漂移，并允许不可信 Task 内容扩大权限。
