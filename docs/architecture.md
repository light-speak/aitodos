# AiTodos 架构设计基线

- 状态：Draft，ADR-0001 至 ADR-0021 已接受；Topic/Task 讨论、显式 Task 反馈意图与只读 Agent 问答、自动 Planning Run、AI Plan Revision/人工审核/批准建 Task、Task Workspace、人工 Review/Diff、显式目标分支集成与同步、本地 Release Tag、项目级 Worker、Task Run/Runner、Run 查询/取消/人工 Retry、Agent Profile Revision、Codex App Server 结构化审批、Agent Session Resume、显式浏览器通知、项目 Skill/MCP 目录与 Run Tool Policy 快照、基础 Context Builder、SQLite FTS Search Projection/Web Search、Estimate/Test、Progress、Task Triage、持久 Clarification/Continuation Run、实际 Usage 统计、可恢复 Finalization、Runner Crash Recovery、按需日志和可断线续传 Run SSE 已实现；项目只读 MCP、MCP 调用审计与受管浏览器资源回收正在推进
- Go module：`github.com/light-speak/aitodos`
- 产品形态：本地优先、单用户、每项目独立运行的 Agent 工作流系统
- 技术基线：Go Control Plane / Runner、React + TypeScript、SQLite WAL、REST + SSE
- 支持平台：macOS 与 Linux；两个平台共用 `flock` 互斥，进程身份分别读取内核进程信息与 Linux procfs；显式打开浏览器分别使用 `open` 与 `xdg-open`

## 1. 目标与非目标

AiTodos 的核心不是复刻 Jira，而是为本地 Coding Agent 提供一个可审计、可恢复、隔离执行的工作流：

```text
Topic Discussion
+ Versioned Plan
+ Searchable Decisions
+ Task Kanban / State Machine
+ Human Review
+ Agent Scheduler
+ Agent Runtime
+ Git Workspace
+ Local Release / Annotated Tag
+ Explainable Progress / Test Evidence
```

系统必须始终能够回答：

- 某个需求当前在讨论、待澄清、方案审批还是执行阶段。
- 当前批准的是哪个 Plan Revision，Task 为什么被这样拆分。
- 哪些 Decision 仍然有效，哪些设定已经被替代。
- 某个 Task 当前是否正在执行。
- 哪个 Runner 正在执行哪个 Run。
- 当前是 Task 的第几轮 AI 执行。
- 一个新 Agent Session 如何在有界 Token 内恢复必要上下文。
- 最近一次执行停在哪个阶段、为什么失败。
- Workspace 当前是否可信、是否被污染。
- 某个 Release 对应哪个不可变 Commit，以及本地 annotated tag 是否创建成功。
- 当前状态是否允许安全重试。

MVP 不考虑多租户、RBAC、团队协作、任意自定义工作流、SaaS、自动 push 和自动合并主分支。Topic、Plan、Label、关系和搜索只服务于 Agent Workflow，不扩展成通用 Jira 替代品。

## 2. 核心架构

```text
Project-local React Web UI
    │ REST commands / SSE events
    ▼
Project-local Go Control Plane
├── Topic / Plan / Task / Review API
├── Domain State Machines / Decision Log
├── Scheduler
├── Recovery Manager
├── Workspace Manager
├── Agent Profile Manager
├── Context Builder / Search Projection
├── Project-local MCP Read Server
└── Artifact Index
    │
    ├── .ats/state.db (SQLite WAL)
    ├── .ats/artifacts
    └── spawn
         ▼
Per-Run Go Runner
├── Lease heartbeat
├── Agent Adapter
├── Process group / timeout / cancel
├── stdout / stderr / event capture
├── Git snapshot and policy check
└── Idempotent finalization
         │
         ▼
Codex / other Agent CLI
```

Control Plane 管理当前项目的业务状态和调度，不直接持有长时间运行的 Agent stdout/stderr 管道。每个 Run 启动一个独立 Runner 进程，一个活跃 Runner 占用一个 Worker 并发槽。不同项目分别执行 `ats start`，在各自保持开启的终端中以前台方式运行，拥有不同 Daemon、SQLite、端口和 Worker 配额，互不发现、互不控制。关闭启动终端或按 `Ctrl+C` 时，对应项目 Daemon 退出。

项目服务的前台生命周期决策见 [ADR-0005](adr/0005-foreground-only-project-daemon.md)，固定端口和显式打开浏览器决策见 [ADR-0006](adr/0006-stable-local-port-and-explicit-browser-open.md)。`ats start` 不自动打开浏览器；`.ats/local.toml` 的 `server.port` 可以固定当前项目端口，`0` 表示随机端口，命令行 `--port` 仅作单次覆盖。

Runner 与 Control Plane 使用同一个 Go module 和二进制：

```text
ats start
ats runner --run-id <id>
```

Claim token 不放入命令行参数，避免出现在进程列表中。Control Plane 通过继承 pipe 向 Runner 传递一次性启动凭据；Runner 启动后使用数据库中的 fencing token 完成心跳和状态更新。

## 3. 模块边界

### Control Plane

负责：

- 当前 Project 配置、Topic、Plan、Task、Message、Decision、Clarification 和 Review 的命令与查询。
- Topic、Plan 和 Task 状态机校验。
- Agent Profile Revision、职责默认值和权限策略。
- Search Projection、Context Builder 和项目级只读 MCP。
- Run 排队、并发配额和公平调度。
- Runner 启动与取消请求。
- 启动恢复和周期性对账。
- REST API、SSE 事件和 UI 静态资源。

不负责：

- 解析具体 Agent CLI 输出。
- 直接修改 Task worktree 中的业务代码。
- 长期持有 Agent CLI 的进程管道。

### Runner

负责一个 Run 的完整执行：

- 获取并续租 Workspace Lease 和 Run Lease。
- 校验 Workspace、Branch、HEAD 和 Git 操作状态。
- 调用 Agent Adapter 构造进程。
- 管理进程组、超时、取消和强制终止。
- 持续写入 stdout、stderr 和规范化事件。
- 采集最终消息、Usage、Git Diff、Commit 和文件变化。
- 使用 claim token 和 lease generation 幂等完成 Run。

### Scheduler

Scheduler 只创建执行决策，不执行 Agent。它必须同时满足：

```text
Project 剩余并发 > 0
Run 业务主体没有其他活跃 Run
需要写代码的 Task Run 拥有可用 Workspace
Run 状态为 QUEUED
```

PLANNING Run 可以作用于 Topic 且不获取 Git Workspace；每个 OPEN Topic 的新输入版本最多自动尝试一次，失败不会无限重试。TRIAGE 和 Task 问答使用的只读 REVIEW Run 作用于 Task 但不获取 Git Workspace；IMPLEMENTATION、REVISION 和需要写代码的 Run 必须作用于 Task 并获取 Workspace。Worker 开关只控制当前项目的新 Claim；正式 Task 自动进入内部 READY，缺少有效评估时先执行 Triage。调度顺序为 Revision、人工发起的 Task 问答、Planning、Triage、Implementation，再在同类工作内按 P0–P3 和排队时间排序。Triage 失败或 Profile 未配置不得永久阻塞 Implementation。系统不提供跨项目统一 Scheduler；机器总并发是所有已启动项目 Worker 配额之和。

### Workspace Manager

负责所有 Git worktree 生命周期操作和仓库级互斥。普通代码编辑可在不同 worktree 并发；`worktree add/remove/repair/prune` 等共享 Git 管理数据的操作必须按规范化后的 `gitCommonDir` 串行化。

### Artifact Store

日志、大 Diff、上下文和最终消息存入当前项目的 `.ats/artifacts`。SQLite 仅保存 Artifact 元数据、索引和校验值，避免日志流导致数据库长期写锁。

用户在 Markdown 输入框粘贴的图片也作为不可变 Artifact 保存。原图用于人工全屏查看和审计；最长边不超过 2000 px 的优化版本用于紧凑预览和后续 Agent Context。Markdown 只保存 `artifact://<id>` 稳定引用，不保存 Base64、绝对路径或临时 URL。默认 Context 只装入当前内容明确引用的优化版本，其他图片按需读取。

## 4. Domain Model

```text
Project
├── Topic
│   ├── Thread / Message
│   ├── Clarification
│   ├── Decision
│   ├── Plan / immutable Revision
│   ├── Planning Run / Agent Session
│   └── derived Task
├── Task
│   ├── Thread / Message
│   ├── Clarification / Decision
│   ├── Workspace / Task Branch
│   ├── Implementation / Revision / Review Run
│   ├── ReviewDecision
│   └── TaskEvent
├── Label
├── Release / annotated Git Tag
└── Search Projection

AgentProfile
├── immutable Revision
├── Purpose / prompt template
├── Adapter / Model / arguments
├── Context / Workspace / Approval policy
└── Environment / Proxy / Provider policy
```

### Project

一个执行过 `ats init` 的本地 Git 仓库，保存默认分支、各 Run purpose 的默认 Agent Profile、默认模型、并发限制、环境变量引用、项目说明和文件策略。每个 Project 自己持有 `.ats/state.db`，不存在全局业务数据库或项目注册表。

初始化默认分支优先使用本地已知的 Remote HEAD，其次使用已有的 `main/master/trunk/develop`，最后才使用当前分支。该判断不访问网络；用户需要自行保证本地 Remote refs 已按需 fetch。

Project 的仓库身份不能只依赖用户输入路径。初始化和启动时解析：

- `repoPath`：用户看到的路径。
- `canonicalRepoPath`：规范化绝对路径。
- `gitCommonDir`：Git 共享管理目录。
- `projectInstanceId`：保存在项目本地数据库中的本机实例标识。

### Topic

代表需要持续讨论、澄清和规划的问题。创建 Topic、追加人类消息或显式请求重试都会形成新的规划输入版本；Scheduler 自动为尚未尝试的 OPEN 版本创建 Planning Run。Topic 可以经历多轮 Planning Run 和多个 Plan Revision，不获得 Git Workspace。Topic 的 Message 是历史事实，当前约束由有效 Decision、开放 Clarification 和批准 Plan 表达。

### Plan

代表 Topic 下的版本化拆分方案。Revision 不可原地覆盖；Planner 只能创建草案，用户批准后才原子生成正式 Task、来源关系和审计事件。

### Task

代表边界明确、可执行和可验收的工作。Task 可以直接创建，也可以从批准的 Plan 或其他 Task 派生。Task 不保存独立的“Agent 是否运行中”布尔值，该信息从活跃 Run 派生，避免双写不一致。

Task 编辑器要求人类显式选择反馈意图：`NOTE` 只保留历史，`DISCUSS` 排队只读 REVIEW Run 并把 Agent 回答追加到同一 Thread，`REQUEST_CHANGES` 进入可审计修订流程。REVIEW 问答不改变 Task 状态，当前问题作为不可裁剪的必选 Context；已验收 Task 的修改请求创建关联后续 Task，不覆盖已经验收的历史。详见 [ADR-0020](adr/0020-task-feedback-turns-and-read-only-agent-discussion.md)。Task 验收后仍停留在 Task Branch；人类通过独立命令 fast-forward 集成到目标分支。分叉时先同步 Task Workspace、重新执行 Revision 和验收，详见 [ADR-0021](adr/0021-explicit-task-integration-and-target-sync.md)。

### Thread、Clarification 与 Decision

Topic 和 Task 可以拥有 Thread。Clarification 表示 Agent 等待用户回答的明确问题；Decision 表示当前有效、可被替代或撤销的结论。普通 Message 不自动获得 Project Instructions 或 Decision 的指令优先级。

### Label 与关系

Label 是 Project 内可自定义的人类分类，只用于展示、搜索、筛选和分组。Task 关系限制为 `DERIVED_FROM`、`PARENT_OF`、`BLOCKS`、`RELATES_TO` 和 `SUPERSEDES`；关系不自动改变调度，除非存在显式策略。

### Workspace

一个 Task 默认拥有一个长期 worktree 和一个 Task Branch。Task 被驳回后，新 Run 继续使用同一 Workspace；需要全新尝试时，由用户显式创建新 Workspace 或新 Task。

### Release

代表 Project 仓库中的一个 SemVer 发布事实。Release 固定保存规范版本、`v<semver>` Tag、来源本地分支和解析后的不可变 Commit SHA；来源分支只用于审计，分支后续移动不会改变 Release。创建 Release 会在本地创建 annotated tag，但不会 push、merge 或自动提交 Workspace 修改。

### Estimate Revision / Test Case / Test Result

Estimate Revision 保存 AI 或人工对 Task 工作量、剩余量、置信度和依据的不可变估算。MVP 尚未实现输入 Revision 和 stale 自动检测。Test Case 描述需要验证的行为；Test Result 保存某次 Run 或人工验证的状态与证据来源。AI 文本声明与 Runner 实际命令结果必须分开标识。

### Task Assessment Revision

Task Assessment Revision 保存 Triage Agent 对标题、六维复杂度、AI 自主度、置信度、依据和拆分建议的不可变评估。复杂度和自主度由后端固定算法从维度评分推导，不能由 Agent 直接指定。评估绑定 Task Assessment Input Version；影响评估输入的需求变化后旧评估作为历史保留并显示过期，单独编辑或锁定标题不使评估过期。它不改变 Points、P0–P3、Agent 权限或模型路由，详见 [ADR-0010](adr/0010-task-triage-title-complexity-and-autonomy.md)。

### Clarification

Clarification 保存 Agent 对 Task 提出的结构化阻塞问题、稳定选项、推荐说明和不可覆盖的人工答案。它与普通评论和 CLI 权限审批分离。开放问题进入全局待回答投影；回答后恢复 Task 并由新的 Continuation Run 继续，详见 [ADR-0011](adr/0011-durable-clarifications-continuation-runs-and-agent-config.md)。

### Progress Projection

Progress Projection 是从 Task、Estimate、Test、Run 和 Review 重建的项目级只读视图，同时展示严格已验收进度和带覆盖率、置信度的 AI 预测，不存储一个可任意修改的“完成百分比”。

### Run

一次独立 AI CLI 执行。Run purpose 为 `PLANNING`、`TRIAGE`、`IMPLEMENTATION`、`REVISION` 或 `REVIEW`，并通过受外键和 CHECK 约束的字段绑定恰好一个 Topic 或 Task。Run 从排队开始形成完整审计记录。Run 创建后，其 Agent Profile Revision、Model、Prompt、Context、环境配置、Workspace 和执行策略快照不可变。Agent 提出阻塞问题时 Run 以 `NEEDS_INPUT` 结束并释放 Worker；人工回答后由新的 `continuation_of_run_id` Run 继续。Agent 进程退出后，Run 先进入 `FINALIZING`；使用 Workspace 的 Run 保存不可变的 HEAD/dirty 前后快照，随后才能写入终态。Task Run 历史默认只读取摘要，失败详情、Artifact 元数据和 Workspace 快照按 Run 读取，stdout/stderr 正文只在人类点击后读取，详见 [ADR-0015](adr/0015-run-finalization-workspace-snapshots-and-lazy-logs.md)。

### AgentSession

Schema v25 的 AgentSession 可以在同一 Topic 或 Task 的多个 Run 之间复用，但不是事实来源。当前要求精确匹配 Profile Revision、Adapter 和 Model；`codex` 使用 CLI Resume，`codex-app-server` 使用 `thread/resume`。Resume 不可用或失败时使 Session 失效，并从持久数据重建 Context，详见 [ADR-0017](adr/0017-codex-app-server-sessions-approvals-and-notifications.md)。

### ApprovalRequest

Schema v26 的 ApprovalRequest 保存同一次 Agent Turn 发出的命令、文件、网络或额外权限请求。开放请求进入全局待处理列表；人类决定通过乐观锁回传 App Server。它与需要新 Continuation Run 的 Clarification 分离，不解析不稳定的终端自然语言。

### ReviewDecision

记录 ACCEPTED 或 REJECTED，以及评论和关联 Run。驳回不是永久终态；它让 Task 进入 `CHANGES_REQUESTED`。

### Event

状态表保存当前状态，事件表保存发生过什么。事件用于审计和 UI 时间线，但 MVP 不采用纯 Event Sourcing；当前状态仍由事务表直接维护。

## 5. 核心不变量

1. 一个 Task 同一时刻最多存在一个活跃 Run。
2. 一个 Workspace 同一时刻最多被一个 Run Lease 持有。
3. Agent 永远不能在 Project 的主 Working Tree 中运行。
4. Run 配置创建后不可变，只允许追加事件和最终结果。
5. `SUCCEEDED` Run 不等于 `ACCEPTED` Task。
6. 所有状态迁移通过领域命令执行，API 不接受任意状态字符串覆盖。
7. 所有 Runner 写操作必须携带当前 claim token 和 lease generation。
8. 系统不自动 push；MVP 不自动 merge 到目标分支。
9. Dirty Workspace 不得被自动强制删除或重置。
10. 无法证明安全的重试必须进入人工处理，而不是自动重复执行。
11. 一个 Topic 同一时刻最多有一个活跃 PLANNING Run。
12. Plan Revision、Agent Profile Revision 和 Run Snapshot 创建后不可原地修改。
13. Planner 只能提出 Plan 和 Task 草案；未经批准不得创建并执行正式 Task。
14. Label 不得隐式改变权限、Agent、状态或调度。
15. Agent Session、Summary 和 Search Projection 都不是事实来源，必须能够从规范数据恢复。
16. Release 创建后不得改绑其他 Branch、Commit 或 Task 集合；同名 Git Tag 必须是指向同一 Commit 的 annotated tag。
17. 领域对象的 `version` 只用于乐观锁，不作为产品 Release 版本展示。

## 6. 数据模型

每个项目的 `.ats/state.db` 独立开启 WAL。所有状态事务应短小，不在事务中执行 Git、文件系统或 Agent CLI 操作。

### project_metadata

```text
id, key, name
canonical_repo_path, git_common_dir, project_instance_id
default_branch, workspace_root
default_planner_profile_id, default_implementer_profile_id
default_revision_profile_id, default_reviewer_profile_id
max_concurrent_runs
environment_config, instructions, file_policy
created_at, updated_at, version
```

### topics / threads / messages

```text
topics:
id, topic_key, title, description, status
current_plan_id, current_summary_id
created_at, updated_at, version

threads:
id, topic_id XOR task_id
created_at, updated_at

messages:
id, thread_id, sequence, author_kind
content, created_at

message_task_links:
message_id, task_id, created_at

topic_task_links:
topic_id, task_id, source_message_id, created_at

task_links:
task_a_id, task_b_id, source_message_id, created_at

task_feedback_turns:
id, task_id, source_message_id, retry_of_feedback_id
intent, status
run_id, response_message_id, failure_message
created_at, updated_at

task_feedback_events:
id, task_id, feedback_id, sequence, status
run_id, response_message_id, failure_message, occurred_at
```

Thread 使用 Topic/Task 外键和 CHECK 约束表达恰好一个主体。评论归属和内容引用分开：每条 Message 恰好属于一个 Topic 或 Task Thread，但可以引用零到多个 Task。Topic–Task 和 Task–Task 关系使用显式外键表；Task–Task 当前为对称的“相关”关系，不隐式改变依赖调度。由 Message 引用产生的主体关系在同一短事务中幂等建立，并保留 `source_message_id`；解除主体关系不删除历史消息引用。

Schema v8 已开放 Topic 与 Task 讨论、评论引用 Task、双向可见的 Topic–Task 关联、Task–Task 关联、Task Workspace 和本地 Release；Schema v29 增加 Task 反馈意图、来源消息、Reviewer Run 和回复消息的真实外键；Schema v30 增加不可变反馈重试链和 Task 内稳定递增的反馈事件。当前 Message 不支持编辑；后续若开放编辑，必须增加 Revision 或审计事件，不静默改写已经进入 Run Context 的历史内容。

### plans / plan_revisions / plan_task_drafts

```text
plans:
id, plan_key, topic_id, status
current_revision_id, approved_revision_id
created_at, updated_at, version

plan_revisions:
id, plan_id, revision_number
summary, rationale, risks
source_run_id, previous_revision_id
created_at

plan_task_drafts:
id, plan_revision_id, draft_key
title, description, acceptance_criteria, priority
recommended_agent_profile_id, proposed_order
```

Plan Revision 和 Task Draft 创建后不可原地修改。批准 Plan 时以一个短事务创建正式 Task、关系和审计事件。

### clarifications / decisions

```text
clarifications:
id, topic_id XOR task_id, source_run_id
question, status, answer_message_id
created_at, answered_at, version

decisions:
id, scope_type, project/topic/task scope foreign key
statement, rationale, status
source_message_id, source_plan_revision_id
superseded_by_id
created_at, updated_at, version
```

Decision 状态为 `ACTIVE`、`SUPERSEDED` 或 `REVOKED`。普通 Message 不替代结构化 Decision。

### labels / topic_labels / task_labels

```text
labels:
id, name, color, description
created_at, updated_at, version

topic_labels: topic_id, label_id
task_labels: task_id, label_id
```

Label 名称在当前 Project 内规范化后唯一。Plan 通过 Topic 继承 Label。

### tasks

```text
id, task_key
title, description, acceptance_criteria
status, priority
source_plan_revision_id, source_plan_task_draft_id
preferred_agent_profile_id
target_branch, base_commit_sha
current_workspace_id, latest_run_id
created_at, updated_at, version
```

Task 更新使用 `version` 乐观锁，避免 UI 重复提交覆盖较新的状态。

`target_branch` 必须是带 Commit 的本地 Branch。创建 Task 时由 UI 默认选择 Project 默认分支；Workspace 创建前可以通过领域命令调整并产生审计事件，绑定 Workspace 后锁定。Remote-only Branch 必须先由用户在 Git 中取得对应本地 Branch，AiTodos 不隐式 fetch 或 checkout。

正式 Task 的优先级为 `P0`、`P1`、`P2` 或 `P3`，默认 `P2`。Task 草稿仍属于 Plan Revision；只有正式 Task 自动进入 Worker 候选集。

### task_estimate_revisions / task_test_cases / task_test_results

```text
task_estimate_revisions:
id, task_id, revision
initial_points, remaining_points, confidence
rationale, assumptions, source_kind, source_run_id, model
input_revision, created_at

task_test_cases:
id, task_id, title, description
kind, required, position, created_by
source_plan_revision_id, created_at, updated_at

task_test_results:
id, test_case_id, run_id
status, evidence_kind
command_summary, exit_code, artifact_id, message
created_at
```

Estimate 和 Test Result 创建后不可原地修改。Progress Projection 只消费最新有效 Estimate 和每个 Test Case 的最新 Result；原始历史继续保留。

### task_reviews

```text
id, task_id
decision, comment, commit_sha
created_at
```

Schema v9 保存不可变人工验收记录。拒绝必须有原因；通过时若 Task 有 Workspace，则 Workspace 必须 clean，并固化验收时完整 HEAD SHA。

### workspaces

```text
id, task_id
path, branch_name, target_branch, base_commit_sha
state, head_sha, dirty
leased_by_run_id, lease_generation, lease_expires_at
last_verified_at
created_at, updated_at
```

Workspace 状态：

```text
PROVISIONING, READY, LEASED, DIRTY, QUARANTINED,
CLEANING, REMOVED, ERROR
```

Schema v7 当前落地 `PROVISIONING`、`READY`、`DIRTY`、`QUARANTINED` 和 `ERROR`；Schema v22 记录每次使用 Workspace 的 Run 在 Finalization 后的不可变 HEAD/dirty 快照。Lease、清理和移除状态随 Run 生命周期实现增加，不能用临时字符串绕过 migration。

### releases / release_tasks

```text
releases:
id, version, tag_name
source_branch, commit_sha
status, failure_message
created_at, updated_at, tagged_at

release_tasks:
release_id, task_id, created_at
```

Release 状态为 `CREATING`、`TAGGED` 或 `FAILED`。数据库先保留 `CREATING` 记录并固定 Commit，再执行 Git；失败可在相同输入下安全重放。系统会把最新通过 Review 的 Commit 作为候选，仅在它是 Release Commit 的 Git 祖先时自动写入 `release_tasks`；该关联证明对应验收 Commit 已包含，但不推断未记录到 Review 的外部修改。

### task_integration_attempts

Schema v31 保存 `INTEGRATE`/`SYNC` 的不可变 Task、Review Commit、目标分支、操作前后 SHA、状态和失败诊断。同一 Task 最多一个 `RUNNING` 尝试。同步成功或冲突都会使旧测试证据过期并进入 `CHANGES_REQUESTED`；daemon 启动时对账未完成记录。

### search_documents

Schema v32 保存可重建的 Search Document 元数据，并以 FTS5 trigram 索引标题、正文和稳定 Key。Topic、Task、Message、Plan Revision 和 Clarification 的创建、更新与删除通过同库触发器在源事务内增量同步；Migration 会回填已有规范数据，读取服务也能在一个事务内全量重建投影。原始 Run 日志、完整 Diff、Artifact 内容、环境变量和凭据不进入该投影。

Schema v33 将正文更新和状态/活动时间更新拆成独立触发器；只改变元数据时不重写 FTS 文本，避免讨论消息和状态刷新产生无效索引写放大。

### runs

```text
id, purpose
topic_id XOR task_id, workspace_id
agent_profile_id, agent_profile_revision_id
agent_session_id, parent_run_id
adapter_type, adapter_version
agent_config_snapshot, model
attempt_number, retry_of_run_id
prompt_artifact_id, context_manifest, context_artifact_id
logical_context_revision, context_delta_artifact_id

status, failure_kind, failure_code, failure_message, retryable
worker_id, claim_token_hash, lease_generation, lease_expires_at

queued_at, claimed_at, process_started_at
started_at, finished_at, duration_ms
pid, process_group_id, process_started_identity
exit_code, exit_signal

stdout_artifact_id, stderr_artifact_id, events_artifact_id
final_message_artifact_id, diff_artifact_id

input_tokens, cached_input_tokens, cache_write_input_tokens
output_tokens, reasoning_tokens, total_tokens
model_requests, peak_input_tokens
context_estimated_tokens
context_included_items, context_omitted_items
cost_amount, cost_currency, cost_source, cost_estimated
pricing_snapshot

git_branch, git_base_sha, git_head_before, git_head_after
commit_shas, changed_files_manifest

cancel_requested_at, cancel_requested_by
created_at, updated_at
```

Token 和 Cost 无法获取时保存 `NULL`，不能用零代替未知值。`input_tokens` 是 Run 累计输入，`cached_input_tokens` 是它的子集；二者都不是单次请求上下文大小。只有 Adapter 能可靠提供时才记录 `model_requests` 和 `peak_input_tokens`。估算 Cost 必须保存价格快照和 `cost_estimated=true`。

Run 使用数据库约束保证恰好绑定一个 Topic 或 Task。PLANNING Run 不得设置 Workspace；需要写代码的 Task Run 必须设置受管 Workspace。

### agent_profiles / agent_profile_revisions / agent_sessions

```text
agent_profiles:
id, name, role, current_revision_id
created_at, updated_at, version

agent_profile_revisions:
id, profile_id, revision_number
prompt_template, model, adapter_config
context_policy, workspace_policy, approval_policy
provider_profile_id, environment_policy
created_at

agent_sessions:
id, topic_id XOR task_id
agent_profile_revision_id, model
adapter_type, external_session_id
status, last_run_id
created_at, updated_at
```

Profile Revision 创建后不可修改。Session 只保存恢复身份和兼容性信息，不保存唯一业务事实。

### task_reviews

验收记录独立保存，不混入 Task description。Review 记录决定、原因、关联 Run 和时间；讨论统一使用 Thread/Message。

### task_relations

```text
from_task_id, to_task_id, relation_type
source_plan_revision_id
created_at
```

关系类型受枚举约束。互斥或对称关系由领域命令验证，不允许 API 任意写字符串。

### topic_events / plan_events / task_events / run_events / workspace_events

每个聚合内使用递增 `sequence`，并建立 `(aggregate_id, sequence)` 唯一索引。事件 payload 使用版本化 JSON，便于以后增加字段。

Schema v23 已落地 `run_events`。Run Claim 和状态迁移在更新当前状态的同一事务内追加事件；`GET /api/runs/{runID}/events` 使用 sequence 作为 SSE `id`，支持 `Last-Event-ID` 和 `after` 断点。Schema v30 的 `task_feedback_events` 使用相同原则解决 Task 问答快速完成时的前端观察竞态；`GET /api/tasks/{taskID}/feedback/events` 在排队或运行期间保持连接，终态事件发出后关闭。前端依靠浏览器 EventSource 续传并按持久状态刷新，不从瞬时活跃 Run 反推反馈结果。原始日志不进入 SSE，详见 [ADR-0016](adr/0016-run-events-and-resumable-sse.md)。

### artifacts / run_artifacts

图片 Artifact Index 保存：

```text
id, kind, original_name
original_media_type, original_relative_path, original_size, original_sha256
optimized_media_type, optimized_relative_path, optimized_size, optimized_sha256
created_at
```

Run Artifact 关联保存：

```text
id, run_id, kind
relative_path, mime_type
size_bytes, sha256
segment_index, truncated
created_at
```

Artifact 路径必须是数据目录下的相对路径，读取时再次校验解析结果没有逃逸 Artifact Root。

Task 页面只默认读取 Run 摘要。Run Detail 读取 Artifact 元数据，不直接内联大内容；stdout/stderr 必须通过独立只读端点按需加载，并再次校验声明大小和 SHA-256。原始日志和完整 Diff 不进入默认搜索投影或 Agent Context。

### summaries / search_documents

Summary 保存主体、来源序号范围、内容哈希、生成 Run 和人工修正信息。Search Document 是可重建的只读 FTS 投影，覆盖 Topic、Plan Revision、Task、Message、Decision、Clarification 和 Run Summary；原始日志与完整 Diff 不进入默认全文索引。

### worker_leases

保存 Runner 身份、Run、PID 身份、heartbeat、租约过期时间和最后错误。PID 身份至少包含 PID、可用时的进程启动时间和 Run nonce，不能只依赖 PID。

### 必要索引和约束

- `project_metadata` 只允许一个当前项目记录。
- `topics(topic_key)`、`plans(plan_key)` 和 `tasks(task_key)` 分别唯一。
- 一个 Topic 最多一个活跃 PLANNING Run，使用部分唯一索引表达。
- 一个 Task 最多一个未终止 Run，使用部分唯一索引表达。
- 一个 Task 最多一个未移除 Workspace。
- `releases(version)` 和 `releases(tag_name)` 分别唯一；同一版本不得改绑 Git 事实。
- Plan 的 `(plan_id, revision_number)` 唯一，批准 Revision 必须属于同一个 Plan。
- Label 规范化名称唯一，所有 Label 关联表使用复合主键。
- Run、Thread、Clarification 和 AgentSession 的主体外键满足恰好一个 Topic 或 Task。
- `runs(status, queued_at)` 调度索引。
- `runs(status)` 和 `runs(agent_profile_id, status)` 并发计数索引。
- 所有外键启用并接受数据库约束校验。

## 7. 状态机

### Topic 与 Plan 状态

```text
Topic OPEN
  ├── Planning Run 回复问题 ─→ OPEN（下一条人类消息形成新版本）
  ├── Planning Run 提交草案 ─→ PLAN_REVIEW
  │                              ├── approve → PLANNED
  │                              └── revise ─→ OPEN（自动开始新一轮）
  └── close ──────────────────→ CLOSED

Plan DRAFT → IN_REVIEW → APPROVED → SUPERSEDED
                       └──────────→ CANCELLED
```

Topic 的活跃 Planning Run 作为派生执行信息展示，不额外维护 `agent_running` 布尔值。Planning 结果包含必须写回讨论的 Agent Reply 和可选 Plan Revision；如果 Run 执行期间 Topic 已出现更新版本，旧 Run 的 Reply 仍保留，但旧 Plan 草案不会覆盖新输入。批准 Plan Revision 和批量创建 Task 必须经过领域命令；Planner Run 只能提交草案。详细并发与恢复语义见 [ADR-0019](adr/0019-automatic-topic-planning-turns.md)。

### Task 状态

```text
READY
   ↓ run claimed
RUNNING
   ├── run failed ───────→ BLOCKED
   ├── cancel ───────────→ CANCELLED
   └── run succeeded
              ↓
            REVIEW
        ┌─────┴──────────┐
      accept           reject
        ↓                ↓
    ACCEPTED       CHANGES_REQUESTED
                           ↓ revision run claimed
                         RUNNING
```

正式 Task 创建即进入 `READY`，不再暴露逐 Task Queue 命令。旧数据库中的 `BACKLOG` 由版本化 migration 连同审计事件迁移到 `READY`。`BLOCKED` 恢复到 `READY` 必须由人工确认，或者由一个已证明安全的基础设施重试策略触发。

### Run 状态

```text
QUEUED → CLAIMED → PREPARING → RUNNING → FINALIZING → SUCCEEDED
```

终态：

```text
SUCCEEDED, NEEDS_INPUT, FAILED, TIMED_OUT,
CANCELLED, LOST, POLICY_VIOLATED
```

取消请求使用独立字段表示。只有 Runner 确认 Agent 进程组退出并完成 Git/日志采集后，Run 才进入 `CANCELLED`。

### 关键迁移规则

| 当前状态 | 命令/事件 | 目标状态 | 关键条件 |
|---|---|---|---|
| TaskDraft | ApprovePlan / CreateTask | READY | 正式 Task 自动进入等待执行，默认 P2 |
| READY | SubmitTaskReview | REVIEW | 人工或外部实现进入验收，不创建虚假 Run |
| READY | RunClaimed | RUNNING | 原子 Claim 且 Workspace 可租用 |
| RUNNING | ClarificationRequested | BLOCKED | Run 以 `NEEDS_INPUT` 终止并保存问题，不继续占用 Lease |
| BLOCKED | AnswerAndQueue | READY / CHANGES_REQUESTED | 保存回答；Scheduler 创建 Continuation Run，不复用旧 Run |
| RUNNING | RunSucceeded | REVIEW | Finalization 完成 |
| RUNNING | RunFailed | BLOCKED | 没有安全自动重试 |
| REVIEW | AcceptTask | ACCEPTED | 用户确认验收；系统先自动提交 dirty Workspace，再固化 Review HEAD |
| REVIEW | RejectTask | CHANGES_REQUESTED | 必须记录驳回原因 |
| CHANGES_REQUESTED | RevisionClaimed | RUNNING | Worker 开启，创建新 Revision Run，不复用旧 Run |

## 8. Run Claim 与并发

并发限制只在当前项目内配置：

```text
project.maxConcurrentRuns = 2（默认）
```

减少并发配置只阻止当前项目的新 Claim，不终止已运行的 Runner。不同项目之间没有共享配额或全局 Claim。一个 Topic 最多一个活跃 PLANNING Run，一个 Task 最多一个活跃 Run；同一项目的并发可以发生在不同 Topic 或 Task 之间。

SQLite Claim 在短事务中完成：

```sql
BEGIN IMMEDIATE;

UPDATE runs
SET status = 'CLAIMED',
    worker_id = ?,
    claim_token_hash = ?,
    lease_generation = lease_generation + 1,
    lease_expires_at = ?,
    claimed_at = ?
WHERE id = ?
  AND status = 'QUEUED'
RETURNING *;

COMMIT;
```

Scheduler 不得在这个事务中启动进程。Runner 后续更新必须使用类似条件：

```text
WHERE id = run_id
  AND lease_generation = expected_generation
  AND claim_token_hash = expected_hash
```

这可以阻止过期 Runner 覆盖新的执行结果。

## 9. Runner 执行协议

### 准备阶段

1. 验证启动凭据并登记 Runner PID 身份。
2. 校验 Run purpose、业务主体、Agent Profile Revision 和 Session 兼容性。
3. 通过 Context Builder 生成有界最终 Prompt、Context Manifest 和必要 Delta。
4. 对需要 Workspace 的 Task Run 获取 Lease。
5. 校验 worktree path、`gitCommonDir`、Branch、HEAD。
6. 检查是否存在未完成的 merge、rebase、cherry-pick 或 bisect。
7. 记录 `git_head_before` 和初始文件状态。
8. 由 Adapter 构造不经过 shell 拼接的 Invocation。

PLANNING Run 不执行 Workspace 步骤，且其 Invocation 不得获得可写 Task worktree。REVIEW Run 是否需要只读 Workspace 由 Profile Revision 的 Workspace Policy 明确指定。

### 运行阶段

1. 创建独立进程组并登记 `process_started_at`。
2. 分别捕获 stdout 和 stderr。
3. Adapter 将 JSONL 或文本解析为规范化 AgentEvent。
4. Runner 周期性续租并检查取消请求。
5. 达到运行超时、空闲超时或策略限制时终止进程组。

Agent 通过 MCP 调用外部工具时，结构化 Adapter 或受管 MCP Gateway 必须生成可审计 Run Event，记录 Server、Tool、开始/结束、耗时、结果状态和脱敏参数摘要；大结果写 Artifact。Generic Adapter 无法解析调用时明确显示未知，不得声称已经追踪。

浏览器 page/context 等有生命周期的资源必须登记为当前 Run 所有的资源租约。AiTodos 只关闭当前 Run 创建的资源，不关闭用户窗口或其他 Run 的资源；不得使用系统 `open` 等无法返回受管资源 ID 的方式替 Agent 启动浏览器。具体边界见 [ADR-0012](adr/0012-traceable-mcp-calls-and-run-owned-browser-resources.md)。

### Finalization

1. 确认 Agent 进程已退出。
2. flush、关闭并校验日志 Artifact。
3. 对使用 Workspace 的 Run 获取 HEAD、Commit、tracked/staged Diff、untracked manifest。
4. 对使用 Workspace 的 Run 执行文件策略检查。
5. 收集 Usage、Cost、最终消息、Clarification 或 Plan Draft。
6. 使用 fencing token 原子写入 Run 终态和事件。
7. 更新 Topic 或 Task 的派生状态。
8. 释放 Workspace Lease。

Schema v27 在进入 `FINALIZING` 时先冻结不可变终态意图。当前 Runner 已在 Agent 成功、失败、超时、取消和 Clarification 路径中刷新 Workspace、写入前后快照并幂等提交终态；Daemon/Runner 崩溃后可以重放。Finalization 在写入终态前仍必须回收已登记的 MCP 资源；ADR-0012 的 MCP 资源租约尚未实现，因此当前不能保证关闭未知外部浏览器。清理失败必须保存诊断并在 Run Detail 显示，不能静默结束。

Finalization 必须幂等。重复执行不得重复创建 Review、Run 或状态迁移。

## 10. Agent Adapter

Adapter 只隔离 Agent CLI 差异，不管理 Git、调度或业务状态。

概念接口：

```text
Probe(ctx, profile) -> AgentCapabilities
BuildInvocation(runSpec) -> ProcessInvocation
ParseEvent(stream, bytes) -> AgentEvent[]
CollectResult(exit, artifacts) -> AgentResult
ClassifyFailure(exit, stderr, events) -> FailureClassification
```

`ProcessInvocation` 包含：

```text
executable
argv[]
stdin
cwd
environment
outputProtocol
```

参数必须以 argv 数组传递，不能拼接 shell 命令字符串。

### Capability

- 非交互执行
- JSONL/结构化事件
- Model Override
- Usage/Cost 上报
- Session Resume
- 内置 Sandbox
- Approval Policy
- Structured Final Output

### 首批 Adapter

1. `codex`：结构化 Adapter，使用非交互和 JSONL 输出。
2. `generic-process`：可配置 executable、argv template、stdin 和 raw/JSONL 模式。

Generic Process Adapter 只保证基本进程、日志、超时和 Git 采集；无法解析的信息保持未知。

当前 Generic Process Profile 的参数模板支持 `{prompt_file}`、`{result_file}`、`{workspace}`、`{model}` 和 `{run_id}`。未使用 `{prompt_file}` 时 Prompt 通过 stdin 传入；Triage 的 Codex 推荐配置使用只读 Sandbox 和 `--output-last-message {result_file}`，让 Runner 校验最终 JSON，而不是要求 Agent 写入代码 Workspace。Implementation、Revision 和 Review 的最终说明通常是自然语言，不把 `--output-last-message` 指向结构化结果文件；需要提交 Estimate/Test 等机器结果时，由 Agent 显式写入 `ATS_RESULT_FILE`。Codex Profile 不得同时配置 `--approve-for-me` 与 `--sandbox`，当前 CLI 会在模型调用前拒绝这组参数。

### Agent Profile

Adapter 是代码能力，Agent Profile 是用户配置。一个 Adapter 可以对应多个 Profile，例如 Planner、Implementer、Reviewer 或使用不同模型、Provider、Proxy 和 Sandbox 策略的同类 Profile。并发只由当前 Project 配置。

Profile 至少包含：

```text
name, role, adapter_type, executable
current_revision_id

Revision:
prompt_template, model, argument_config
context_policy, timeout_config
sandbox_policy, workspace_policy, approval_policy
proxy_profile_id, provider_profile_id
environment_policy
```

Profile 在 UI 中可编辑，但编辑必须创建新 Revision。内置职责 Prompt 可以查看、比较和恢复默认值；系统安全约束不可被 Profile Prompt 覆盖。模板变量使用固定白名单，不允许执行代码、读取任意文件或展开 Secret。Profile 启用前必须执行 Probe，记录 CLI 路径、版本和能力。CLI 版本改变后重新 Probe；解析器必须按版本容错，不能把未知事件当成成功。

项目维护 Skill Catalog 和 MCP Server Catalog；Agent Profile Revision 只引用项目目录中的稳定能力 ID，并保存 Skill、Server、Tool allowlist 与 required/optional 策略。Run 创建时固化解析后的 Tool Policy、版本/哈希与 Probe 结果。Task、Message、检索内容和 Agent 输出不得扩大能力范围。完整决策见 [ADR-0013](adr/0013-project-skills-mcp-catalog-and-profile-tool-policy.md)。

Schema v19 已实现项目 Skill 路径/内容哈希、Codex MCP 配置名引用、Profile Revision 能力绑定和 Run Tool Policy 快照。MVP 不复制本机 MCP 的连接参数或 Secret；运行前通过 Codex CLI 校验配置是否存在，并对未选择的 Server 生成禁用配置。Codex `-c` 的覆盖路径不接受带引号的名称段，因此 MVP MCP 配置名只允许 ASCII 字母、数字、下划线和连字符。选中的 Skill 进入 Context Builder；optional Skill 可在失效或软预算不足时省略，required Skill 失效时在模型调用前失败。必需 Context 超出本地估算不会阻止 Run。Skill 文件变化后必须由用户重新校验并递增目录版本，已经创建的 Profile Revision 不原地修改。

Project 分别保存 PLANNING、TRIAGE、IMPLEMENTATION、REVISION 和 REVIEW 的默认 Profile。最终 Prompt 的固定分层、Revision 兼容性和权限边界遵循 [ADR-0003](adr/0003-topic-plan-task-and-role-based-agent-runs.md) 与 [ADR-0010](adr/0010-task-triage-title-complexity-and-autonomy.md)。

## 11. Context、Search 与 MCP

Agent Session 只用于尽力恢复对话，不是事实来源。规范领域数据、Artifact Index、Search Projection、Web Search、Run Context Builder 和 Project-local MCP 使用同一套读取服务：

```text
Domain Data / Artifact Index
           ↓
Search Projection / Context Read Service
      ├── Web Search
      ├── Context Builder
      └── Project-local MCP
```

Context 按 L0 固定规则、L1 当前工作集、L2 近期增量和 L3 历史档案分层。默认只装入有界的 L0、L1 和 L2；旧 Plan、旧 Run、完整日志和完整 Diff 通过搜索或工具按需读取。Acceptance Criteria、有效 Decision、开放 Clarification 和批准 Plan 不得只存在于自动 Summary 中。

每个 Profile Revision 固化内部软预算、输出预留、安全余量、近期 Message 数量、检索数量、关联对象数量和 Artifact 片段上限。普通配置不要求用户调整 Token 预算。Context Builder 按规则、当前对象、有效 Decision、批准 Plan、开放问题、近期增量、检索摘要和 Artifact 片段的顺序装入，并记录每项内容的来源、Revision、哈希、Token 估算、是否采用和省略原因。Token 估算只服务于去重和低价值历史取舍，不是实际 Usage，也不得裁掉安全规则、当前任务、验收标准、项目规则、开放问题或当前修订所依据的驳回意见；必需内容超过软预算仍完整发送。

Search Projection 使用项目 SQLite FTS5 trigram，当前覆盖 Topic、Task、Message、Plan Revision 和 Clarification，支持类型、状态、当前版本、更新时间和游标分页过滤。Decision 与 Run Summary 尚无完整规范表，必须在对应事实模型落地后再加入投影，不能从日志或 Agent 文本推断。Search Projection 是可重建的只读投影，不是事实来源；原始日志、完整 Diff 和 Artifact 内容不进入默认全文索引。

MCP 第一阶段只提供当前项目的有界只读搜索和读取能力，不直接暴露数据库，也不提供批准 Plan、启动 Run、验收 Task 或删除对象等高影响命令。检索内容始终视为不可信数据，不得提升到系统安全或 Project Instructions 层。

详细决策见 [ADR-0004](adr/0004-durable-context-search-mcp-and-token-budget.md)。

## 11.1 整体进度与测试证据

整体进度页区分按非取消 Task 数计算的严格已验收进度和按 Points 计算的 AI 预测进度。未估算 Task 不进入 Points 分母，但必须展示估算覆盖率。MVP 尚未实现 Estimate stale 检测。测试通过率只根据结构化 Test Result 计算，并明确区分 Runner 命令、人工确认和 Agent 自报。Task 详情默认只展开仍需处理的 required Test Case，已验证项和 optional 项折叠展示。

Runner 只把可信 Adapter 提供的结构化命令完成事件作为候选证据；Agent Result 报告的命令必须精确匹配本 Run 观察到的命令，且结果与退出码一致，才保存为 `COMMAND`，否则保存为 `AGENT_REPORT`。正常验收要求 required Test Case 的最新结果全部为 `PASSED` 且证据为 `COMMAND` 或 `HUMAN`；MVP 不提供 Override。详细决策见 [ADR-0009](adr/0009-ai-estimates-progress-and-test-evidence.md)。

整体进度页同时展示真实 Run Usage：累计输入、其中缓存、非缓存输入、缓存命中率、输出、采集覆盖率和按 Purpose 汇总。Schema v20 使用独立 `run_usage` 表保存 Codex 结构化事件提供的可空指标。旧 Codex JSONL 使用最终 `turn.completed`；Codex App Server 按当前 `turnId` 对 `thread/tokenUsage/updated` 的累计快照去重，再累加每次模型请求的 `last`，因此 Session Resume 不会把历史 Turn 重复计入新 Run，并可得到模型请求数和单次输入峰值。累计输入不得标注为“上下文大小”；Cost 无可靠来源时保持未知。详细决策见 [ADR-0014](adr/0014-actual-usage-and-quality-first-soft-context-budget.md)。

## 12. Proxy、Provider 与环境变量

Network Proxy 和 LLM Provider/Gateway 必须分开建模。

### Network Proxy Profile

模式：

```text
INHERIT  继承 AiTodos/Runner 进程环境
EXPLICIT 使用 Profile 显式配置
OFF      清除代理变量
```

可映射的变量：

```text
HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, NO_PROXY
SSL_CERT_FILE, NODE_EXTRA_CA_CERTS
```

默认使用 `INHERIT`，使 Agent 沿用执行当前项目 `ats start` 时已有的代理。每个项目可以在 `.ats/local.toml` 中选择不同的 Server 端口、Agent 命令、模型、Worker 数量、Proxy 和 Provider。

### Provider Profile

用于表达：

```text
base_url
API key environment reference
static/dynamic headers
provider-specific model mapping
```

MVP 不在 SQLite 或 TOML 保存 API Key 明文。只允许引用已有环境变量或 Agent CLI 登录状态。后续再接操作系统 Keychain。

### 环境优先级

```text
Runner 安全基础环境
→ Global environment
→ Project environment
→ Agent Profile environment
→ Run-specific override
```

Run 快照只保存变量名、来源、是否存在和脱敏哈希，不保存秘密值。保留变量覆盖审计，但日志和错误输出仍需做最佳努力脱敏。

## 13. Git Workspace 生命周期

### 仓库事实

Repository API 只读取当前本地仓库，返回仓库根目录、规范化 `gitCommonDir`、Git 版本、项目默认分支、Remote HEAD、当前 Branch/HEAD/dirty、Upstream、ahead/behind、本地分支、Git 用户身份和 Remote 列表。Remote URL 在传输前移除 URL UserInfo、Query 与 Fragment，避免把常见凭据形式带入 UI、日志或 Agent Context。

这些信息不触发 fetch，因此 Remote HEAD 和 ahead/behind 只表达本地已知状态。系统不把“刷新仓库信息”解释为网络授权，也不自动 push。

### 创建

```text
验证 Git 仓库
→ 校验 Task 目标是已有 Commit 的本地 Branch
→ 解析目标分支为固定 Base SHA
→ 获取 gitCommonDir 仓库锁
→ 创建唯一 Task Branch
→ 创建 linked worktree
→ 校验 worktree 和共享 Git 目录
→ 保存 Workspace=READY
```

推荐命名：

```text
branch: aitodos/<project-key>/<task-key>-<short-id>
path:   <repo-root>/.ats/worktrees/<task-id>
```

### Run 前校验

- Workspace 路径在受管根目录内。
- `.git` 指向预期 `gitCommonDir`。
- 当前 Branch 与数据库一致。
- HEAD 与上一个 Finalization 记录一致。
- Workspace 没有其他有效 Lease。
- Git 没有进行中的危险操作。

### Run 后采集

- HEAD before/after。
- 新增 Commit SHA。
- tracked、staged 和二进制变化。
- untracked 文件清单及受限大小内的内容 Artifact。
- changed file manifest。
- allow/deny path policy 违规。

### 验收和清理

`.ats/worktrees` 被项目本地 `.ats/.gitignore` 忽略。Workspace 虽位于项目目录内，Run 前仍必须通过真实路径和 `gitCommonDir` 校验，删除时只允许操作 `.ats/worktrees` 下的已登记路径。

MVP 默认不允许 Agent push，不隐式 merge，也不自动删除 Workspace。Scheduler 领取正式 Task 后按需准备 Workspace；人工在 REVIEW 状态查看相对 Base Commit 的文件清单与按需 Diff，执行“验收通过”时系统自动提交 dirty Workspace，并让 Review 固化 HEAD。验收后由人类显式 fast-forward 集成；目标分支分叉时先同步 Task Workspace，再执行 Revision、测试和验收。Release 只接受已经存在于来源分支上的 Commit，并根据 Git 祖先关系自动关联能够证明已包含的已验收 Task。

Dirty Workspace 清理前必须保存恢复 Artifact；无法证明目标路径和 Git 身份时，将 Workspace 标记为 `QUARANTINED`，禁止自动删除。

### 安全边界

Worktree 只能隔离 Git 工作目录，不能阻止进程写 Workspace 外路径。MVP 依赖 Agent 内置 Sandbox、文件策略和前后状态检查；容器、VM 或 OS 级强隔离属于后续扩展。

## 14. 日志与执行事件

stdout、stderr 分别保存，同时产生统一顺序的事件流：

```text
sequence, timestamp, stream, event_type, payload
```

建议每个日志 segment 上限 10 MiB，每个 Run 配置总日志上限。达到上限后记录 `truncated=true`，保留头部摘要和末尾诊断信息，不能让无限输出耗尽磁盘。

Artifact 使用临时文件写入，完成后 flush、校验并原子 rename。当前 SSE 从持久 Run Event 增量读取，使用 SSE `id` 支持浏览器断线续传；日志正文仍通过 Artifact API 按需读取，不通过 SSE 逐行广播。

## 15. Cancel、Timeout 与资源限制

Runner 为 Agent 创建独立进程组：

1. 收到取消或超时后发送温和终止信号。
2. 等待配置的 grace period。
3. 仍未退出则终止整个进程组。
4. 确认退出后采集日志和 Git 状态。
5. Finalize 为 `CANCELLED` 或 `TIMED_OUT`。

限制至少包括：

```text
runTimeout
idleOutputTimeout（可选，默认关闭）
maxLogBytes
maxAutomaticRetries
maxRunsPerTask
maxCumulativeDurationPerTask
maxCumulativeTokensPerTask（Usage 可用时）
maxCumulativeCostPerTask（Cost 可用时）
maxInputTokens
reservedOutputTokens
contextSafetyMarginTokens
```

## 16. Crash Recovery 与安全重试

进程启动和数据库更新之间无法做到严格 exactly-once，因此恢复策略必须保守。

### 启动恢复

```text
扫描所有非终态 Run
→ 检查 Runner heartbeat 和 lease
→ 校验 PID、进程启动身份和 Run nonce
→ 检查 Agent 子进程组
→ 对账日志 Artifact
→ git worktree list 对账
→ 校验 Workspace Branch / HEAD / dirty 状态
→ 恢复跟踪、标记 LOST 或隔离 Workspace
→ 修复 Topic/Task 派生状态
```

### 重试矩阵

| 情况 | 自动处理 |
|---|---|
| CLI 明确未启动 | 可重新进入队列 |
| Spawn 临时失败且 Workspace 未变化 | 可有限自动重试 |
| CLI 已启动、已确认退出、Workspace 未变化 | 创建新的 Retry Run |
| CLI 已修改 Workspace | 不自动重试，人工确认 |
| CLI 是否启动不明确 | 标记 LOST，人工确认 |
| Policy violation | 不自动重试 |
| Agent/测试业务失败 | 默认不自动重试 |

每次真正重新调用 AI 都创建新 Run，并通过 `retry_of_run_id` 建立关系。只有能证明 CLI 从未启动时，原 Run 才能重新排队。

Schema v27 已实现 Runner 15 秒心跳/45 秒 Lease、PID + 内核启动身份 + Run nonce 校验、旧 Runner 继续观察、冻结 Finalization 重放和保守 `LOST`。恢复不会自动调用 Agent；MCP 外部资源的恢复边界仍以 ADR-0012 为准。

## 17. API

API 使用领域命令，避免通用 PATCH 任意改状态。

当前已实现的 REST 接口：

```text
GET    /api/project
POST   /api/project/workers
GET    /api/progress

POST   /api/topics
GET    /api/topics
GET    /api/topics/{topicId}
GET    /api/topics/{topicId}/messages
POST   /api/topics/{topicId}/messages
GET    /api/topics/{topicId}/relations
POST   /api/topics/{topicId}/relations
DELETE /api/topics/{topicId}/relations/{taskId}

POST   /api/tasks
GET    /api/tasks
GET    /api/tasks/{taskId}
PUT    /api/tasks/{taskId}/title
GET    /api/tasks/{taskId}/messages
POST   /api/tasks/{taskId}/messages
POST   /api/tasks/{taskId}/feedback
GET    /api/tasks/{taskId}/feedback
GET    /api/tasks/{taskId}/feedback/events
POST   /api/task-feedback/{feedbackId}/retry
GET    /api/tasks/{taskId}/topics
POST   /api/tasks/{taskId}/topics
DELETE /api/tasks/{taskId}/topics/{topicId}
GET    /api/tasks/{taskId}/relations
POST   /api/tasks/{taskId}/relations
DELETE /api/tasks/{taskId}/relations/{relatedTaskId}
GET    /api/tasks/{taskId}/quality
GET    /api/tasks/{taskId}/assessment
POST   /api/tasks/{taskId}/estimates
POST   /api/tasks/{taskId}/test-cases
POST   /api/tasks/{taskId}/test-cases/{testCaseId}/results
GET    /api/tasks/{taskId}/workspace
POST   /api/tasks/{taskId}/workspace
GET    /api/tasks/{taskId}/changes
GET    /api/tasks/{taskId}/changes/file
POST   /api/tasks/{taskId}/submit-review
GET    /api/tasks/{taskId}/reviews
POST   /api/tasks/{taskId}/reviews
POST   /api/tasks/{taskId}/workspace/commit

GET    /api/agent-profiles
GET    /api/agent-profiles/{profileId}/revisions
POST   /api/agent-profiles/{profileId}/revisions

GET    /api/project/capabilities
POST   /api/project/capabilities/skills
POST   /api/project/capabilities/skills/{skillId}/refresh
POST   /api/project/capabilities/mcp-servers

GET    /api/clarifications
GET    /api/tasks/{taskId}/clarifications
POST   /api/clarifications/{clarificationId}/answer

GET    /api/runs
GET    /api/runs/{runId}
GET    /api/runs/{runId}/logs
GET    /api/runs/{runId}/events
POST   /api/runs/{runId}/cancel
POST   /api/tasks/{taskId}/retry
GET    /api/approvals
GET    /api/runs/{runId}/approvals
POST   /api/approvals/{approvalId}/decision

GET    /api/git
GET    /api/releases
POST   /api/releases
GET    /api/tasks/{id}/integration
POST   /api/tasks/{id}/integration
POST   /api/tasks/{id}/integration/sync
POST   /api/artifacts/images
GET    /api/search
```

Web Search 已提供有界接口和全局入口；项目只读 MCP Server、Decision/Run Summary 投影和 Label 接口仍是后续阶段，当前不得由 UI 或 MCP 假定存在。Topic Planning Run、Run 查询、详情、按需日志、SSE、取消、人工 Retry、Task 反馈查询/续传/失败重试和结构化 Approval 已提供有界接口。Plan Revision/人工审核/批准建 Task、Topic/Task Clarification 与 Agent Tool Policy 已提供有界命令；Tool Policy 不等同于 MCP 调用审计，后者仍按 ADR-0012 推进。

写命令携带目标聚合的 `version` 或 `If-Match`，冲突返回 409。批准 Plan、批量生成 Task、回答 Clarification 和状态迁移必须使用领域命令，不提供通用状态 PATCH。

MCP 复用同一应用读取服务，但不等同于 REST 数据库镜像。第一阶段提供 `search_items`、`get_topic`、`get_plan`、`get_task`、`get_thread`、`get_decisions`、`get_related_items`、`get_task_runs`、`get_run_summary` 和 `get_context_bundle`。

## 18. UI 信息架构

### Project Header

- 当前 Project 名称、仓库路径和健康状态。
- 活跃/排队 Run 数和本项目并发使用情况。
- 当前项目各 purpose 的默认 Agent、Model 和 Proxy 模式。

### Topic 与 Plan

- 全局创建入口只要求用户描述“想做什么”，默认创建 Topic，同时保留显式“直接创建 Task”选项。
- 创建时不要求用户填写标题；领域层从内容首个非空行生成 `PROVISIONAL` 临时标题。Triage Agent 在 Worker 开启后生成正式标题和复杂度评估；人工明确填写或编辑的标题标记为 `HUMAN` 并锁定，后续 AI 不得覆盖。
- Topic 与 Task 概念对用户可见，用于明确“讨论/规划”和“可执行/可验收”的边界；Plan 作为 Topic Detail 内的版本化方案展示，不作为顶层导航。
- Topic 列表展示状态、Label、开放 Clarification、当前 Plan 和最近活动。
- Topic Detail 当前展示描述、持久 Thread、Plan Revision、人工审核和批准后生成的关联 Task；有效 Decision、Topic Clarification 和 Planning Run 随对应领域能力补齐。Task Detail 已展示 Task Agent 的 Clarification 历史和就地回答入口。
- Topic Detail 展示关联 Task；Task Detail 展示关联 Topic 和关联 Task。两侧均可搜索、添加、移除和点击跳转，Topic–Task 关系双向可见。
- Topic 和 Task 评论均可引用多个 Task；发送后自动建立对应 Topic–Task 或 Task–Task 主体关系，消息内保留可点击的来源引用。
- Topic、Task 和 Message 内容使用无工具栏 Markdown 输入，不引入富文本文档模型。粘贴自 `<pre>/<code>` 或明显为代码的多行文本时自动插入 fenced code block；长代码块默认只显示前 4 行，可手动展开和收起。
- 粘贴图片时保留原图并生成优化版本，正文只显示紧凑的 `[图片]` 入口；桌面悬停显示不超过 `320 × 240` 的预览区域，点击后在视口范围内全屏查看原图。原始 HTML 不执行，外部链接只允许 `http`、`https` 和 `mailto`。
- Markdown 输入采用无内层边框、不可手动拖拽尺寸的 composer 画布；新建事项使用大尺寸画布，讨论回复使用紧凑画布。页面未处于输入或弹窗状态时按 `N` 打开新建事项；`Ctrl+N` / `Cmd+N` 保留给浏览器；输入画布中使用 `Ctrl+Enter` / `Cmd+Enter` 提交。
- Plan Review 展示方案差异、Task Draft、关系、推荐 Agent、顺序、风险和批准操作。
- Planner 输出默认是草案；正式创建 Task 前必须显示将创建的对象和关系。

### Project Kanban

UI 使用四列投影，不直接把内部状态机全部暴露为列：

```text
待办      = READY | CHANGES_REQUESTED | BLOCKED
进行中    = RUNNING
待验收    = REVIEW
已完成    = ACCEPTED
归档筛选  = CANCELLED
```

卡片继续显示“待完善、可执行、需修改、已阻塞”等精确状态标签；已评估的卡片显示 Complexity 与 Autonomy Badge，看板支持按 C1–C5 或“未评估”筛选。复杂度不改变列、优先级或调度顺序。拖动或操作仍必须转换成领域命令，UI 分组不得覆盖内部状态字符串。

拖动卡片必须转换成明确领域命令；不允许绕过状态机直接写目标状态。

### Task Detail

- Description 和 Acceptance Criteria。
- 当前 AI 评估的 C1–C5、A0–A3、六维评分、置信度、依据、假设与拆分建议；标题可由人工编辑并锁定。
- Status、持久讨论、关联 Topic、关联 Task、Clarification、Decision 和 Review History。
- Workspace、Branch 和 Base SHA。
- Run History。
- 当前 Agent、Model、Runner 和执行阶段。
- 实时日志与结构化事件。
- Git Diff、Commit、Changed Files。
- Accept、Reject、Cancel、Retry 操作。

### Search 与 Agent Settings

- 全局搜索统一检索 Topic、Plan、Task、Message、Decision、Clarification 和 Run Summary。
- 支持类型、Label、状态、更新时间和“仅当前有效内容”过滤。
- 结果显示匹配片段、来源、Revision、更新时间和稳定链接。
- Agent Settings 可编辑 Profile Revision、职责 Prompt、Model、运行参数和权限策略，并支持查看最终 Prompt、与内置默认值对比和恢复默认值；Token 软预算由系统管理，不作为普通人工配置项。
- Run Detail 显示 Context Manifest、Token 预算、采用/省略原因、Session Resume 和实际 Usage。

## 19. 仓库结构草案

```text
AiTodos/
├── cmd/
│   └── aitodos/
├── internal/
│   ├── controlplane/
│   ├── domain/
│   │   ├── topic/
│   │   ├── plan/
│   │   └── task/
│   ├── scheduler/
│   ├── runner/
│   ├── agent/
│   ├── contextbuilder/
│   ├── search/
│   ├── mcp/
│   ├── workspace/
│   ├── artifact/
│   ├── storage/
│   └── transport/
├── migrations/
├── web/
│   ├── src/
│   └── package.json
├── docs/
│   └── adr/
├── go.mod
├── pnpm-workspace.yaml
└── README.md
```

被管理项目运行时目录：

```text
<project-root>/.ats/
├── project.toml       # 可提交的项目配置
├── local.toml         # 本机 Server/Agent/Proxy 配置，忽略
├── .gitignore
├── state.db           # 当前项目业务数据库，忽略
├── artifacts/         # 当前项目 Run Artifact，忽略
├── runtime/           # Daemon metadata 与 lock，忽略
└── worktrees/         # Task linked worktrees，忽略
```

领域层不能依赖 HTTP、SQLite、Git CLI 或具体 Agent Adapter。Control Plane 和 Runner 可以共享领域类型，但进程间只通过持久化状态和版本化启动协议通信。

## 20. 测试策略

默认按 TDD 实施。

### Domain

- Topic/Plan/Task/Run/Workspace 状态迁移表驱动测试。
- 非法迁移、重复命令和幂等测试。
- 并发限制和重试判定测试。
- Plan 批准后批量创建 Task、关系和事件的原子性测试。

### Storage

- 真实 SQLite 临时数据库迁移测试。
- 并发 Claim 测试，证明同一 Run 只能被一个 Runner Claim。
- fencing token 和过期 Lease 测试。
- Topic/Task 单活跃 Run 约束和 Run 主体 CHECK 约束测试。
- Search Projection 增量更新、删除和全量重建一致性测试。

### Git Workspace

- 每个测试创建独立临时 Git 仓库。
- 并发 worktree、dirty Workspace、Branch 错位和恢复测试。
- 所有删除测试验证目标始终位于受管 Workspace Root。

### Runner

- 使用可控 fake Agent executable。
- 覆盖正常退出、超时、无输出、日志爆量、忽略终止信号和崩溃。
- 覆盖 Control Plane 重启、Runner 重启和 Finalization 重放。

### Adapter Contract

- 固定 CLI 事件 fixture。
- 未知事件、破损 JSONL、版本差异和 Usage 缺失测试。
- Session Resume、Session 失效和 Prompt/Profile 不兼容时新建 Session 测试。

### Context、Search 与 MCP

- Context 优先级、职责隔离、预算截断、稳定排序和内容去重测试。
- Summary 来源范围、增量更新、过期和人工修正测试。
- 检索内容不能提升为系统指令的 Prompt Injection 边界测试。
- MCP 分页、结果上限、项目隔离、只读能力和敏感数据测试。
- MCP 调用事件的脱敏、顺序、失败与未知能力测试。
- 浏览器 context/page 的 Run 归属、终态关闭和 Crash Recovery 重放清理测试。
- Usage 可用和不可用时的 Token、Cache 与未知值展示测试。

### UI

- 状态命令和冲突处理组件测试。
- Kanban 关键流程端到端测试。
- Topic 讨论、Clarification、Plan Review、批准拆 Task、Label 和全局搜索流程。
- 实际验证 SSE 断线重连、Run 日志和 Cancel 行为。

## 21. MVP 边界

MVP 包含：

- `ats init/start/status/stop` 和仓库健康检查。
- 每项目独立 Kanban、SQLite、Daemon 和本机可配置端口。
- Topic、Thread、Clarification、Decision、Plan Revision 和人工批准流程。
- Task CRUD、Message、Review、Label、关系和状态机。
- SQLite 持久 Queue、Claim、Lease 和 Recovery。
- Planner、Implementer、Reviewer 职责编排。
- Project 默认职责 Profile、版本化 Prompt、Context Policy 和并发配置。
- 每 Task 一个 worktree。
- 绑定不可变 Commit 的本地 SemVer Release 与 annotated tag。
- Codex 和 Generic Process Adapter。
- Model、环境、Proxy 和 Provider Profile。
- Agent Session Resume 与无法恢复时的 Context 重建。
- SQLite FTS 全文搜索、结构化过滤和可重建 Search Projection。
- 分层 Context Builder、增量 Summary、质量优先的软 Token Budget 和 Context Manifest。
- 当前项目只读 MCP 搜索与 Context Bundle。
- Run History、日志、Diff、Commit 和 Usage。
- Timeout、Cancel、Kill、手工 Retry。
- SSE 实时事件。

MVP 不包含：

- 自动 push、自动 merge 和 PR。
- 远程 Worker。
- 全局 Project Hub、跨项目统一调度和全局并发上限。
- Docker/VM 强隔离。
- 多用户、RBAC。
- 任意自定义 WorkItem 类型和自定义状态机。
- 根据 Label 隐式触发 Agent 或调度。
- 无人批准的 Planner 自动建 Task、自动分配和自动执行链。
- 自动依赖 DAG 调度；Task 关系第一阶段只用于追溯和展示。
- MCP 高影响写命令。
- 向量数据库和语义 Embedding 搜索。
- Agent 内部自主多 Agent 编排。
- 无限自动修复循环。
- 桌面壳和系统 Keychain 集成。

## 22. 实施阶段

1. 固化项目级 Daemon ADR、CLI 协议和 `.ats` 目录格式。
2. TDD 实现 `ats init/start/status/stop`、SQLite migration 和进程生命周期。
3. 初始化 React workspace，实现项目级 Kanban 纵向切片。
4. TDD 实现 Topic、Plan、Task、Clarification、Decision、Label、关系和状态机。
5. 实现 Thread、Plan Review、人工批准和批量创建 Task 的纵向切片。
6. 实现版本化 Agent Profile、职责默认值和 Prompt 配置 UI。
7. 实现项目 Skill/MCP 目录、Profile Tool Policy 和 Run 能力快照。
8. 实现 SQLite FTS Search Projection 与 Web Search；随后复用读取层实现只读 MCP。
9. 实现分层 Context Builder、Summary、软 Token Budget、Context Manifest 和 Session 模型。
10. TDD 实现 SQLite Claim 和 Git Workspace Manager。
11. 实现本地 Release、annotated tag 和 Git 审计 UI。
12. 实现 Runner、进程组、Artifact 和 Recovery。
13. 实现 Codex Adapter 与 Generic Process Adapter。
14. 实现 Review API、SSE、Task Detail 和 Run Detail。
15. 完成并发、Context、MCP、Proxy、取消、重启和磁盘限制验证。

## 23. 尚待后续 ADR 决定

- SQLite Go driver 和迁移工具。
- HTTP router 和 OpenAPI 生成方式。
- React Router、数据请求和拖拽库。
- “验收并提交”领域命令的 Commit 作者、消息模板和失败恢复细节；Agent 不得静默 commit 的边界已由 ADR-0007 确定。
- macOS/Linux/Windows 的第一阶段支持范围。
- 桌面封装采用 Wails、Tauri 或保持本地 Web。

这些决定涉及第三方依赖或产品行为，进入实现前分别评审，不在本设计中默认安装依赖。
