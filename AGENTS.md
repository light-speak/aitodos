# AGENTS.md

本文件适用于公开产品仓库 `light-speak/aitodos`。子目录存在更具体的 `AGENTS.md` 时，子目录规则可以补充本文件，但不得降低这里的安全要求。

本仓库的 `main` 是稳定分支。初始发布完成后，所有变更从功能分支通过 Pull Request 合并；私有 AI 工作区、真实项目数据和私有验收测试不得进入本仓库。

## 1. 沟通

- 使用中文回复，简洁、直接，不写客套话。
- 不确定时明确说“不确定”，不得猜测事实、API 或运行结果。
- 存在会显著改变实现方向的关键歧义时，先问一个问题再动手。
- 给出多个方案时，标注推荐项并说明原因和 trade-off。
- 先说明结果或结论，再说明必要的实现细节。
- 不生成 emoji，除非用户明确要求。

## 2. 工作流

- 按“理解 → 计划 → 实现 → 验证 → 汇报”执行，不看到任务就直接修改。
- 修改前检查相关代码、测试、配置、文档和现有风格。
- 默认采用 TDD：新功能先写失败测试，再写实现；Bug Fix 必须先补回归测试。
- 修改后必须运行与变更相关的测试和 linter。
- Go 代码至少运行相关 `go test`、`gofmt` 和 `go vet`。
- TypeScript/React 代码至少运行项目已有的测试、类型检查和 linter。
- UI 修改必须检查实际行为；项目具备浏览器或 E2E 能力后，不以静态阅读代替验证。
- 不留下 TODO、占位实现、空壳接口或“以后再处理”的临时方案。
- 只展示和解释改动部分，不在回复中整篇重贴文件。
- 优先编辑现有文件；新文件必须具有清晰、长期存在的职责。

## 3. 依赖与工具

- 不擅自安装、升级或替换依赖；需要新增依赖时，先说明用途、替代方案和影响，并等待用户确认。
- 优先复用项目已有依赖和标准库，不重复造轮子。
- 不因为个人偏好替换项目已经确定的框架、数据库或工具链。
- 不执行会修改用户全局环境的安装命令。

## 4. Git 规则

- 可以自行执行 `git status`、`git diff`、`git log`、`git add` 和常规分支查看操作。
- 执行 commit 前必须先列出“将提交的文件清单 + commit message”，等待用户明确确认。
- 绝不 push，push 由用户执行。
- 不 force push，不使用 `--no-verify`。
- commit 后不 amend；需要修正时创建新 commit。
- Commit message 使用英文在前、中文在后，带类型前缀，例如：

  ```text
  feat: add run history / 添加执行历史
  ```

- 类型使用 `feat`、`fix`、`test`、`docs`、`refactor`、`perf` 或 `chore`。
- 不使用 `git reset --hard`、强制 checkout 或其他破坏性命令清理用户改动。
- 工作树存在与当前任务无关的改动时保留它们，不覆盖、不回滚、不混入本次修改。

## 5. 项目技术基线

- Go module：`github.com/light-speak/aitodos`。
- Control Plane 和 Runner 使用 Go。
- Web UI 使用 React + TypeScript + Vite，包管理器使用 pnpm。
- 数据库使用 SQLite WAL。
- API 使用 REST，实时事件和日志使用 SSE。
- 系统采用本地优先、单用户、每项目独立 Daemon 的模块化单体架构。
- Control Plane 与 Runner 使用同一个 Go module 和可执行文件。
- 每个 Run 使用独立 Runner 进程。
- 每个 Task 默认拥有一个长期 Git worktree，多个 Run 顺序复用。
- 每个项目的数据、配置、Artifact、runtime metadata 和 worktree 都保存在项目 `.ats` 目录。
- 不创建全局业务数据库、全局 Project registry 或全局 Scheduler。

架构基线见：

- `docs/architecture.md`
- `docs/adr/0001-go-control-plane-and-per-run-runner.md`
- `docs/adr/0002-project-local-daemon-and-state.md`
- `docs/adr/0003-topic-plan-task-and-role-based-agent-runs.md`
- `docs/adr/0004-durable-context-search-mcp-and-token-budget.md`
- `docs/adr/0005-foreground-only-project-daemon.md`
- `docs/adr/0006-stable-local-port-and-explicit-browser-open.md`

实现与架构文档冲突时，不得静默偏离。先说明冲突；需要改变已接受决策时，新增或更新 ADR。

## 6. 项目定位与边界

AiTodos 的核心是：

```text
Topic Discussion
+ Versioned Plan
+ Searchable Decision
+ Task Kanban / State Machine
+ Human Review
+ Agent Scheduler
+ Agent Runtime
+ Git Workspace
```

当前不实现：

- 多租户、RBAC、企业权限和复杂组织结构。
- 自动 push。
- 默认自动 merge 到主分支。
- 无限自动重试或无限 Agent 循环。
- 无法审计的后台代码修改。

UI、Label、关系和传统项目管理能力只服务于 Agent Workflow，不扩展成通用 Jira 替代品，不实现任意自定义 WorkItem 类型和状态机。

## 7. 领域模型约束

- Project、Topic、Plan、Task、Workspace、Run、Review、Decision 和 Agent Profile 是独立领域概念，不得合并成一个通用 Job 模型。
- Topic 负责长期讨论和需求澄清；Plan 负责不可变 Revision 和 Task 草案；Task 只表达可执行、可验收工作。
- Planner 只能创建 Clarification、Plan Revision 和 Task 草案；未经人工批准不得创建并执行正式 Task。
- Label 只用于展示、搜索、筛选和分组，不得隐式改变 Agent、权限、状态或调度。
- Task 拆分创建新 Task 和显式关系，不覆盖原 Task；关系第一阶段不自动形成依赖调度。
- Task 状态和 Run 状态必须分离。
- Topic 的 Agent 执行状态从活跃 Planning Run 派生，不维护重复布尔字段。
- Run 成功只表示执行及 Finalization 成功，不表示 Task 已验收。
- Run 必须记录 `PLANNING`、`IMPLEMENTATION`、`REVISION` 或 `REVIEW` purpose，并通过受约束外键绑定恰好一个 Topic 或 Task。
- 一个 Topic 同一时刻最多有一个活跃 Planning Run。
- 一个 Task 同一时刻最多有一个活跃 Run。
- 一个 Workspace 同一时刻最多被一个 Run Lease 持有。
- Run 创建后的 Agent、Model、Prompt、Context、环境、Workspace 和策略快照不可变。
- Plan Revision、Agent Profile Revision 和 Run Snapshot 创建后不可原地修改。
- Agent Session 可以跨 Run 复用，但不是事实来源；无法 Resume 时必须能从持久数据重建 Context。
- 驳回 Task 时创建 Review 记录，并进入 `CHANGES_REQUESTED`；不得覆盖历史 Run。
- Task 的 Agent 执行状态从活跃 Run 派生，不维护重复布尔字段。
- 所有状态变化通过领域命令和状态机执行，不允许 API 任意 PATCH 状态字符串。
- 状态更新和对应审计事件应在同一个数据库事务中完成。
- 当前状态表和事件审计表并存；MVP 不采用纯 Event Sourcing。

## 8. Go 代码规范

- 使用现代、清晰、惯用的 Go。
- 所有 Go 文件经过 `gofmt`。
- 函数保持小而单一，尽量不超过 50 行；复杂流程拆成可测试步骤。
- 普通失败返回 `error`，不使用 panic 处理预期错误。
- 错误必须包含有助于定位阶段和对象的信息，同时避免泄露 Secret。
- 长时间操作和 I/O 接受并传播 `context.Context`。
- Goroutine 必须有明确退出条件和所有者，不创建无法停止的后台循环。
- Channel、锁和 Lease 的职责要清晰，禁止用 sleep 猜测同步状态。
- 导出标识符需要有有效注释；注释使用中文，标识符使用英文。
- 同一逻辑重复三次时抽取通用函数，但不为假想复用提前抽象。
- 领域层不能依赖 HTTP、SQLite、Git CLI 或具体 Agent Adapter。

## 9. TypeScript 与 React 规范

- TypeScript 不使用 `any`；不确定类型使用 `unknown` 并显式收窄。
- 变量、函数、组件和类型使用英文命名；注释使用中文。
- 优先使用 `async/await`，不使用冗长的 `.then` 链。
- 组件保持聚焦，业务状态机和 API 数据处理不得堆在展示组件中。
- Server State 与本地 UI State 分离。
- Kanban 拖动必须转换成领域命令，不能直接覆盖 Task 状态。
- SSE 必须支持断线重连、事件序号恢复和重复事件幂等处理。
- 时间、Duration、Token、Cost 和未知值在 UI 中明确区分；未知值不得显示为零。
- UI 错误必须显示可操作信息，不静默吞掉冲突、取消和恢复异常。

## 10. 数据库与持久化

- SQLite 启用 WAL、foreign keys 和合理的 busy timeout。
- 数据库事务必须短小；事务内不得运行 Git、Agent CLI 或长时间文件操作。
- Schema 变更使用版本化 migration，不在启动逻辑中散落临时 ALTER。
- Claim、状态迁移、Lease 和 Finalization 必须有并发测试。
- 使用数据库约束表达关键不变量，包括唯一性、外键和单活跃 Run。
- Topic/Task 多主体引用不得只使用无外键约束的多态 ID，必须使用真实外键和 CHECK 约束。
- Plan 批准后创建 Task、关系和审计事件必须在同一个短事务完成。
- Search Projection 和 Summary 是可重建投影，不得成为唯一事实来源。
- Token 和 Cost 获取不到时保存 `NULL`，不得用零代表未知。
- Cost 估算必须保存价格快照、币种、来源和 `estimated` 标识。
- stdout、stderr、大 Diff 和 Context 存入 Artifact Store；不得将高频日志逐行写入主数据库。
- Artifact 使用受管根目录下的相对路径，读写时防止路径逃逸。
- Artifact 先写临时文件，完成后校验并原子替换。

## 11. Scheduler、Claim 与 Lease

- 并发只使用当前 Project 的 `max_workers`，默认值为 2，不叠加 Agent Profile 并发限制。
- 减少并发数只阻止当前项目的新 Run，不自动杀死当前 Runner。
- 一个 Topic 最多一个活跃 Planning Run，一个 Task 最多一个活跃 Run；项目并发只发生在不同业务主体之间。
- 当前项目内按优先级和排队时间排序；不得实现隐式跨项目调度。
- Claim 使用短事务和条件更新，禁止“先查询再无条件更新”。
- 每个 Claim 使用不可预测 token 和递增 `lease_generation`。
- Runner 的状态更新必须同时校验 Run ID、claim token 和 lease generation。
- Lease 过期不等于可以直接重跑；必须先确认 Runner 和 Agent 进程状态。
- 一次真正重新调用 AI 必须创建新 Run，并关联 `retry_of_run_id`。
- 只有能证明 Agent CLI 从未启动时，原 Run 才允许重新排队。

## 12. Runner 与进程管理

- 每个 Run 由独立 Runner 负责完整生命周期。
- Agent 进程必须运行在独立进程组中，取消和超时作用于整个进程组。
- Claim token、API Key 和其他 Secret 不得放入命令行参数。
- Agent Invocation 使用 executable 和 argv 数组，不拼接 shell command string。
- Runner 必须分别采集 stdout 和 stderr，并维护统一递增事件序号。
- Runner 必须处理正常退出、spawn 失败、超时、取消、强制终止和日志爆量。
- Finalization 必须幂等，可在进程或应用重启后安全重放。
- PID 校验不能只比较 PID，还要包含可用的进程启动时间和 Run nonce。
- 无法确认 Agent 是否执行过时，将 Run 标记为 `LOST` 并要求人工处理，不自动重试。
- 所有后台循环必须有停止机制、超时和最大重试次数。

## 13. Agent Adapter

- Adapter 只处理 Agent CLI 差异，不负责 Scheduler、Task 状态机或 Git Workspace 生命周期。
- Adapter 接口围绕 Probe、Invocation、Event Parse、Result Collect、Failure Classification 和 Capability 展开。
- Adapter 无法提供的 Usage、Cost、Session 或结构化事件保持未知，不伪造结果。
- CLI 版本变化后必须重新 Probe；解析器遇到未知事件时容错并保留原始输出。
- Generic Process Adapter 只保证进程、日志、超时和 Git 采集，不假设具备结构化能力。
- Agent Profile 是用户配置，Adapter 是代码能力；不得把 Profile 特例硬编码进 Adapter。
- Project 分别配置 Planning、Implementation、Revision 和 Review 默认 Profile。
- Agent Profile Prompt 可由用户在 UI 修改，但修改必须创建新 Revision，只影响未来 Run。
- 系统安全规则不可由 Profile Prompt 覆盖；Planner 不得获得可写 Task Workspace，Reviewer 默认只读。

## 14. Proxy、Provider 与 Secret

- Network Proxy 与 LLM Provider/Gateway 分开建模。
- Network Proxy 支持 `INHERIT`、`EXPLICIT` 和 `OFF`。
- 默认 `INHERIT`，让 Agent 沿用启动 AiTodos 时已有的代理环境。
- Provider Profile 表达 Base URL、API Key 环境变量引用、Header 和模型映射。
- MVP 不在 SQLite 保存 API Key、Proxy 密码或 Bearer Token 明文。
- Run 配置快照只保存环境变量名、来源、存在性和脱敏信息。
- 日志脱敏是补充保护，不得以“之后会脱敏”为理由主动记录 Secret。
- 不同 Agent 对 Proxy、Sandbox、Approval 和 Resume 支持不同，必须通过 Capability 表达。

## 15. Git Workspace 安全

- Agent 永远不能在 Project 主 Working Tree 中运行。
- Workspace 创建、移除和修复按规范化 `gitCommonDir` 加仓库级锁。
- Workspace 只能位于当前项目 `.ats/worktrees` 下，并必须被 `.ats/.gitignore` 忽略。
- Branch 名和 Workspace 路径包含 Project/Task 唯一标识，不能仅依赖用户标题。
- Run 前校验 worktree path、`gitCommonDir`、Branch、HEAD 和未完成 Git 操作。
- Run 后采集 HEAD before/after、Commit、tracked/staged Diff、二进制变化和 untracked manifest。
- Dirty Workspace 不得自动强制删除、reset 或 checkout。
- 无法证明 Workspace 路径和 Git 身份时，标记 `QUARANTINED`。
- Agent 默认禁止 push；系统自身绝不自动 push。
- Agent commit 策略在独立 ADR 确认前不得擅自实现。
- Worktree 不是强安全沙箱；不得声称它能阻止 Workspace 外写入。

## 16. 日志、Artifact 与可观测性

- stdout、stderr 原始流和规范化 Run Event 都要保留。
- 日志必须支持分段、总量限制和截断标记，防止耗尽磁盘。
- 截断时保留足够的头部上下文和尾部错误诊断。
- Run Event 使用稳定 sequence，SSE `id` 与其对应。
- Failure 使用结构化 kind、code、message 和 retryable，不只保存一段 stderr。
- Run 必须记录开始、结束、Duration、Exit Code/Signal、Agent、Model、Git 和 Usage 信息。
- 任何无法采集的字段保持未知，并在 UI 明确显示。

## 17. Crash Recovery

- Control Plane 启动时对账非终态 Run、Runner Lease、进程身份、Artifact 和 Git worktree。
- 应用重启后不得仅凭数据库 `RUNNING` 就认为进程仍然存在。
- Runner 存活时恢复跟踪，不启动重复 Runner。
- Runner 已死且 Agent 是否启动不明确时标记 `LOST`。
- Workspace 已变化或状态不可信时标记 `QUARANTINED`，等待人工决策。
- 自动重试只用于已分类、明确安全、次数受限的基础设施错误。
- 不允许无限重试、无限 Agent 轮次或无上限恢复循环。

## 18. Context、Search 与 MCP

- Agent Session 不是事实来源；Project Instructions、有效 Decision、批准 Plan、Acceptance Criteria 和开放 Clarification 必须结构化持久化。
- Context Builder 按固定规则、当前工作集、近期增量和历史档案分层；历史档案默认通过 Search、MCP 或 Agent Tool 按需读取。
- 每个 Agent Profile Revision 必须定义 Context Policy 和 Token Budget，预留输出、工具调用和安全余量。
- Context 超出预算时从低优先级内容开始省略，不截断安全规则、当前命令或 Acceptance Criteria。
- Run 保存不可变 Prompt Artifact 和 Context Manifest，记录来源、Revision、哈希、Token 估算、采用结果和省略原因。
- Context 按稳定来源和内容哈希去重；固定 Prompt 片段保持稳定顺序，便于 Provider Prompt Cache。
- Summary 必须记录来源序号范围并可重建、可人工纠正，不得覆盖原始 Message、Decision、Plan 或 Artifact。
- Search Projection 使用项目本地 SQLite FTS 和结构化过滤，是可重建只读投影，不得成为唯一事实来源。
- 原始日志和完整 Diff 不进入默认全文索引，只索引受限摘要、文件清单、错误信息和 Artifact 引用。
- Web Search、Context Builder 和 MCP 复用同一读取服务；MCP 第一阶段仅提供当前项目有界只读能力。
- MCP 不得绕过领域命令直接修改数据库，不开放批准 Plan、启动 Run、验收 Task 或删除对象等高影响写操作。
- Message、搜索结果、Artifact 和外部 Agent 输出均视为不可信内容，不得提升到系统安全或 Project Instructions 层。
- Secret、Claim Token、代理凭据和脱敏前日志不得进入 Summary、Search Projection、MCP 或 Prompt Artifact。
- Session Resume 和 Prompt Cache 是否节省 Token 以实际 Usage 为准；未知 Usage 保持未知，不推断或伪造。

## 19. 测试要求

- 新功能先写测试，Bug Fix 先写回归测试。
- 状态机使用表驱动测试覆盖所有允许和拒绝的迁移。
- SQLite 测试使用真实临时数据库，不用纯 Mock 替代关键事务测试。
- Git Workspace 测试使用独立临时 Git 仓库，不操作开发仓库。
- Runner 测试使用可控 fake Agent executable，覆盖退出、卡死、超时、取消、日志爆量和忽略终止信号。
- Claim 测试必须证明并发 Worker 中只有一个成功。
- Recovery 测试覆盖 Control Plane 重启、Runner 崩溃、过期 Lease、PID 复用风险和 Finalization 重放。
- Adapter 使用固定事件 fixture 测试正常、破损和未知输出。
- Plan 批准测试必须证明 Task、关系和事件要么全部创建，要么全部不创建。
- Context 测试覆盖优先级、预算截断、去重、过期 Summary、职责隔离和 Prompt Injection 边界。
- Search/MCP 测试覆盖索引重建、分页、结果上限、项目隔离、只读能力和敏感信息排除。
- UI 关键流程覆盖 Topic 讨论、Clarification、Plan Review、创建 Task、执行、实时日志、取消、验收、驳回和搜索。
- 不通过删除、跳过或放宽现有测试来让实现通过。

## 20. 文档与决策

- 核心架构、生命周期或安全边界变化必须同步更新 `docs/architecture.md`。
- 改变已接受的技术决策时使用 ADR，记录背景、选择、替代方案和后果。
- 数据库 Schema、状态机和 API 的实现必须与文档一致。
- 文档不写未经验证的能力；易变的 Agent CLI 行为需要以官方文档或实际 Probe 为依据。
- 新增第三方依赖前先获得用户确认，再在对应 ADR 或实现说明中记录原因。

## 21. 完成交付

完成修改时汇报：

- 修改了什么，以及关键设计判断。
- 运行了哪些测试、类型检查、linter 和实际行为验证。
- 哪些验证未运行及其原因。
- 是否存在与当前任务无关的工作树改动。
- 不自动 commit；需要 commit 时先按 Git 规则请求确认。
