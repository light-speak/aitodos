# ADR-0004：持久上下文、统一搜索、MCP 与 Token Budget

- 状态：Accepted
- 日期：2026-08-18
- 影响：定义跨 Run/Session 的记忆来源、上下文压缩和 AI/人类统一检索边界

## 背景

Agent CLI Session 可能失效、不支持 Resume、因 Prompt 或 Model 变化而不能安全复用，也可能在恢复后仍对完整历史上下文计费。仅依赖 Session 会使长期讨论无法审计或恢复；每次重放全部 Topic、Comment、Plan、Run 日志和 Diff 又会造成大量无效 Token。

AiTodos 还需要让人和外部 AI 在不同时间搜索以前的设定、决策、方案和工作结果。Web Search、Run Context Builder 和 MCP 若分别拼接数据，会产生不同的事实视图和权限边界。

## 决策

### 1. 持久领域数据是事实来源

Agent Session 不是事实来源。当前有效信息必须持久化为：

- Project Instructions 和版本化 Agent Profile。
- Topic/Task Message。
- 有状态的 Clarification。
- 有效或已废弃的 Decision。
- 不可变 Plan Revision 和批准记录。
- Task、Review、Run Summary、Artifact 元数据和关系。

原始 Message 和 Artifact 永久保留在项目数据边界内，压缩只影响发送给 Agent 的上下文，不删除审计历史。

### 2. Decision 是结构化、可追溯对象

Decision 至少包含：

```text
scope_type, scope_id
statement, rationale
status: ACTIVE | SUPERSEDED | REVOKED
source_message_id, source_plan_revision_id
superseded_by_id
created_at, updated_at, version
```

历史讨论中的文字不会自动获得指令优先级。Agent Context 优先使用当前有效 Decision；旧评论和已废弃 Decision 只作为按需参考。

### 3. Summary 是可重建投影，不覆盖事实

Topic、Thread、Plan 和 Task 可以维护版本化 Summary：

```text
subject_type, subject_id
source_sequence_from, source_sequence_to
content, content_hash
generated_by_run_id
corrected_by_user
created_at
```

Summary 只处理上一个覆盖序号后的增量，达到阈值或构建下一次 Context 时按需更新，不为每条 Message 启动无上限后台调用。Summary 必须保留来源范围，允许重建、人工纠正和识别过期。

Acceptance Criteria、有效 Decision、未解决 Clarification 和批准 Plan 不得只存在于自动 Summary 中。

### 4. Context Builder 使用分层、预算化和职责化策略

上下文分为：

```text
L0 固定规则：系统约束、Project Instructions、Agent Profile
L1 当前工作集：当前 Topic/Task、有效 Decision、批准 Plan、开放问题
L2 近期增量：上次 Run 后的新 Message、状态和代码变化摘要
L3 历史档案：旧 Revision、旧 Run、完整日志、完整 Diff
```

默认 Prompt 只装入 L0、L1 和受限 L2。L3 通过 Search、MCP 或 Agent Tool 按需读取。

不同 purpose 使用不同 Context Policy：

- PLANNING 不默认读取完整代码 Diff、Runner 日志和无关 Task Thread。
- IMPLEMENTATION 读取当前批准 Plan、Task、Acceptance Criteria、相关 Decision 和必要代码上下文。
- REVISION 额外读取最近 Review 和前一 Run 结果，不重放全部历史。
- REVIEW 优先读取 Acceptance Criteria、Diff、测试结果、Decision 和风险摘要，默认不写 Workspace。

### 5. 每个 Profile 定义 Token Budget

Context Policy 至少包含：

```text
max_input_tokens
reserved_output_tokens
safety_margin_tokens
recent_message_limit
retrieval_limit
related_item_limit
artifact_excerpt_limit
```

可用输入预算为模型上下文上限减去输出、工具调用和安全预留。模型上限或 Tokenizer 无法可靠获取时使用 Profile 显式上限和保守估算，不猜测 Provider 能力。

Context Builder 按以下优先级装入内容：

1. 系统安全和运行规则。
2. 当前 Topic/Task。
3. 有效 Decision。
4. 当前批准 Plan 和 Acceptance Criteria。
5. 未解决 Clarification。
6. 上次 Run 后的增量。
7. 检索得到的相关摘要。
8. Artifact 片段。

超出预算时从低优先级内容开始省略，不截断安全规则、当前命令或 Acceptance Criteria。大日志、大 Diff、旧 Plan 和远端关系默认只发送摘要、清单和可查询引用。

### 6. Context Manifest 必须可解释和去重

Run 保存最终 Prompt Artifact 和不可变 Context Manifest。Manifest 对每个候选片段记录：

```text
source_type, source_id, revision
content_hash, reason, priority
estimated_tokens
included, omitted_reason
```

Context Builder 按稳定来源 ID、Revision 和内容哈希去重，避免 Topic Summary、Plan、Task Description 和 Decision 重复表达同一内容。固定 Prompt 片段使用稳定顺序和稳定文本，以便支持 Prompt Cache 的 Provider 获得更高命中率。

### 7. Session 可以跨 Run，但必须可替换

```text
Topic 或 Task
└── AgentSession
    ├── Run 1
    ├── Run 2
    └── Run 3
```

AgentSession 绑定业务主体、Agent Profile Revision、Model 和 Adapter Capability。每次 Agent 调用仍创建新 Run。支持可靠 Resume 时，后续 Run 优先发送上次 Run 后的 Context Delta；不支持或无法确认时，从持久数据重建新 Session。

Run 记录 `agent_session_id`、外部 Session ID、父 Run、逻辑 Context Revision、实际发送 Delta 和按需读取记录。Resume 是否节省 Token 只根据 Adapter 返回的实际 Usage 判断，不从 Capability 推断。

修改 Profile Prompt、Model、关键 Project Instructions 或不兼容 Context Policy 后默认 Fork 新 Session。Session 失效不得导致 Topic、Plan、Task 或 Decision 丢失。

### 8. 建立统一、可重建的 Search Projection

Topic、Plan Revision、Task、Message、Decision、Clarification 和 Run Summary 最终投影为统一只读 Search Document。领域写模型保持独立。第一阶段只索引已有规范事实模型中的 Topic、Task、Message、Plan Revision 和 Clarification；Decision 与 Run Summary 在对应规范表落地前不得从 Agent 输出或日志推断生成。

第一阶段使用项目 SQLite FTS5 trigram 和结构化过滤，不引入外部搜索服务或 Embedding 依赖。Search Projection 从允许检索的规范表重建，不是事实来源；Artifact 只保留稳定引用，不把内容或原始文件名默认写入全文索引。

FTS 索引更新使用同库触发器，与规范写入保持同一短事务；不在写事务中读取 Artifact、生成 Summary 或调用模型。搜索读取设置结果数、匹配片段、字符预算和分页上限；查询同时使用 entity/status/time 等结构化过滤与 FTS 排序。第一阶段全量重建在单个 SQLite 事务中完成，读者只能看到重建前或重建后的完整投影；真实项目基准显示事务时间不可接受时，再引入影子索引切换，不能预先增加两套触发器与动态表管理复杂度。

搜索至少支持：

- 关键词和短语。
- entity type、Label、状态和更新时间过滤。
- 当前有效内容优先。
- 可选择包含已废弃 Decision、旧 Plan Revision 和终态历史。
- 稳定 ID、可读 Key、匹配片段、来源、Revision 和更新时间。
- 游标分页和有界结果数量。

原始 stdout、stderr 和完整 Diff 不默认写入全文索引，只索引受限摘要、错误信息、文件清单和 Artifact 引用。

Embedding 只作为 FTS 后的可选第二阶段召回，不替代结构化过滤和关键词搜索。启用前必须满足：

- 不新增常驻外部服务；Provider、本地模型和维度通过独立配置显式选择。
- 只处理允许进入 Search Projection 的文本，Secret 和原始日志仍排除。
- 按规范化内容哈希去重，模型或维度变化创建新索引 Revision，不原地混用向量。
- 写路径只追加待索引 ID；Embedding 在有界批次中异步生成，可暂停、限速、恢复和全量重建。
- 保存队列深度、索引覆盖率、批次延迟、检索延迟和 FTS/向量命中来源；索引落后不得阻止领域写入、Run 或 MCP 读取。
- 查询先执行结构化过滤与 FTS；仅在结果不足或显式语义查询时，对有界候选执行向量召回，再使用确定性规则融合，避免全表扫描。
- 设置项目级磁盘、并发、请求时限和返回字符预算。超时或 Provider 不可用时立即退化为 FTS，不返回空白知识库。

在 FTS5 基线尚未完成真实数据量基准前，不引入 Embedding 依赖或 Schema。

### 9. Web Search、Context Builder 和 MCP 共用读取服务

```text
规范领域数据和 Artifact Index
            ↓
Search Projection / Context Read Service
       ├── Web Search
       ├── Context Builder
       └── Project-local MCP Server
```

MCP 第一阶段只读，并由当前 Project Daemon 暴露当前项目数据，不创建全局 Project Registry。首批能力：

```text
search_items
get_topic
get_plan
get_task
get_thread
get_decisions
get_related_items
get_task_runs
get_run_summary
get_context_bundle
```

查询必须支持 `summary`/`full` 明细级别、结果上限、游标和 Token/字符预算。`get_context_bundle` 接受业务主体、purpose 和预算，返回有来源的有界上下文，而不是导出整个数据库。

第一阶段不通过 MCP 暴露批准 Plan、启动 Run、验收 Task、删除对象等高影响命令。后续写能力只能以领域命令形式增加，并要求版本校验、幂等键、审计和显式 Approval Policy。

### 10. 检索内容始终是不可信数据

Message、搜索结果、Artifact 和外部 Agent 输出可能包含类似指令的文本。Context Builder 和 MCP 必须携带来源与内容边界，不得把检索内容拼入系统安全层。只有显式 Project Instructions、Profile Revision 和用户确认的 Decision/Plan 获得对应优先级。

Secret、环境变量值、Claim Token、代理凭据和脱敏前日志不得进入 Search Projection、Summary、MCP 或 Prompt Artifact。

### 11. Token 和检索行为必须可观测

Run 在能力可用时记录：

```text
input_tokens, cached_input_tokens, cache_write_input_tokens
output_tokens, reasoning_tokens, total_tokens
model_requests, peak_input_tokens
context_estimated_tokens
context_included_items, context_omitted_items
session_resumed
```

未知值保存 `NULL`。`cached_input_tokens` 是 `input_tokens` 的子集，二者都计入实际输入；Run 累计输入不能冒充单次上下文大小。UI 显示 Context Manifest、软预算、采用/省略原因、Session 是否恢复和实际 Usage，便于判断 Summary、Resume、Prompt Cache 和按需读取是否真正降低消耗。质量优先的软预算修订见 [ADR-0014](0014-actual-usage-and-quality-first-soft-context-budget.md)。

## 后果

正面影响：

- 新 Session 可以从持久化事实恢复，不依赖特定 Provider 的隐藏状态。
- 原始历史完整保留，但不会在每个 Run 中重复发送。
- 人类搜索、Agent Context 和 MCP 对有效版本与来源的理解一致。
- Token 使用可解释、可度量，并能按职责控制。
- 搜索索引损坏时可以从规范数据重建。

代价：

- 需要 Summary、Search Projection、Context Manifest 和 Session 兼容性管理。
- Context 选择策略必须覆盖预算、去重、过期和 Prompt Injection 测试。
- SQLite FTS 更新需要与领域事务保持可恢复的一致性。
- MCP 即使只读也需要项目身份、分页、限额和敏感数据测试。

## 被否决的方案

### 每次发送完整历史

最容易实现，但 Token 随时间无界增长，并把大量旧方案、日志和已废弃决策混入当前工作。

### 只依赖 Agent Session Resume

无法跨 Adapter 提供一致恢复和计费语义，Session 失效后也无法审计或重建。

### 只保存自动摘要

节省空间但会产生摘要漂移，且无法证明当前结论来自哪条消息或哪个 Plan Revision。原始事实和结构化 Decision 必须保留。

### 第一阶段引入向量数据库

会增加依赖、Embedding 成本、隐私和索引一致性问题。项目内 FTS、结构化过滤和按需读取足以建立第一阶段基线。

### MCP 直接暴露数据库或全部写命令

会绕过领域状态机、版本校验和 Approval Policy。MCP 必须复用应用服务和读取模型，写能力后续逐项评审。
