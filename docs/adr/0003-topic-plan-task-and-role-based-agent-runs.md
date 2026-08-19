# ADR-0003：Topic、Plan、Task 与按职责编排的 Agent Run

- 状态：Accepted
- 日期：2026-08-18
- 影响：扩展 ADR-0001 的 Task 中心执行模型，但不改变 Per-Run Runner 和每项目独立调度边界

## 背景

并非所有用户输入都已经具备可执行范围和验收标准。很多工作需要先由人和 Agent 反复讨论、澄清约束、形成方案，再拆成一个或多个可独立执行和验收的 Task。

如果把讨论、方案和执行全部放进 Task：

- 未明确的需求会过早进入执行状态机。
- 多次方案修改会覆盖历史，难以说明 Task 为什么这样拆分。
- Planner、Implementer 和 Reviewer 的职责、权限和上下文无法清晰区分。
- 为了保持对话连续性而复用 Agent Session 时，Session 容易被误当成业务事实来源。

AiTodos 仍不以复刻 Jira 或提供任意自定义工作流为目标。新增对象只服务于 Agent 需求分析、任务拆分、执行和审计。

## 决策

### 1. 使用三个独立领域对象

```text
Topic（长期议题与讨论）
└── Plan（版本化拆分方案）
    └── Task（可执行、可验收单元）
```

- Topic 保存待解决的问题、讨论线程、当前摘要、Clarification 和 Decision。
- Plan 保存 Planner 生成或用户编辑的版本化方案。批准的 Plan Revision 可以原子生成多个 Task。
- Task 只表达边界明确、可以排队执行并由人验收的工作。
- Topic、Plan 和 Task 不合并成通用 Job，Workspace、Run、Review 和 Agent Profile 继续保持独立。

Plan 是 Topic 下的独立可搜索对象，但其 Revision 不可原地覆盖。Task 可以脱离 Plan 由用户直接创建，也可以由批准的 Plan 派生。

### 2. 使用 Label 表达人类分类

Project 内允许用户创建 Label，并关联 Topic 和 Task。Bug、Feature、Refactor、Frontend 等分类使用 Label，而不是增加任意自定义领域类型或状态机。

Label 默认只用于展示、搜索、筛选和分组。Plan 继承所属 Topic 的 Label，不单独复制标签关系。Label 不得隐式改变 Agent、权限、状态或调度；需要按 Label 路由时必须建立显式、可审计的 Automation Rule。

### 3. Topic 和 Task 都可以拥有讨论线程

- Topic Thread 用于长期需求讨论和方案演进。
- Task Thread 用于实现阶段的补充说明、Agent 提问和 Review 反馈。
- Message 只表示讨论内容，不自动成为当前有效指令。
- 当前约束必须进入 Project Instructions、批准的 Plan、Acceptance Criteria 或有效 Decision。
- Agent 提问使用 Clarification，并明确记录 `OPEN`、`ANSWERED` 或 `CANCELLED`。

### 4. Plan 使用不可变 Revision

Plan 状态至少包括：

```text
DRAFT → IN_REVIEW → APPROVED → SUPERSEDED
                    └────────→ CANCELLED
```

修改已提交或已批准方案时创建新 Revision。批准操作记录批准人、时间和源 Revision；从 Plan 创建 Task 时在同一个数据库事务中创建 Task、来源关系和审计事件，防止只创建部分任务。

Planner 只能提交 Plan 草案和推荐，不得仅凭自然语言输出直接批准 Plan、启动 Run 或验收 Task。

### 5. Run 具有职责和业务主体

一次真正调用 Agent 仍然创建一个不可变 Run。Run 增加 `purpose`：

```text
PLANNING
IMPLEMENTATION
REVISION
REVIEW
```

- PLANNING Run 作用于 Topic，不获得可写 Git Workspace。
- IMPLEMENTATION 和 REVISION Run 作用于 Task，并使用该 Task 的长期 Workspace。
- REVIEW Run 作用于 Task，默认只读；是否允许独立 Workspace 由显式策略决定。
- 一个 Topic 同一时刻最多有一个活跃 PLANNING Run。
- 一个 Task 同一时刻最多有一个活跃 Run。

物理数据库不得只保存无外键约束的多态 `subject_id`。Run 使用受约束的 Topic/Task 外键和 CHECK 约束表达恰好一个业务主体。

### 6. Control Plane 编排职责，不实现 Agent 内部多 Agent 系统

Control Plane 根据 Run purpose 选择默认 Agent Profile、权限和 Context Policy：

```text
Planner     分析需求、提出 Clarification、生成 Plan Revision
Implementer 修改 Task Workspace 并验证结果
Reviewer    只读检查 Diff、测试和验收标准
Generalist  用户显式选择的通用职责
```

Planner 可以为 Task 草案推荐 Agent Profile 和执行顺序，但 Scheduler 只调度用户已批准并正式创建的 Task。第一阶段不允许 Planner 无人确认地创建、分配和执行正式 Task。

### 7. Agent Profile 可在 UI 配置且必须版本化

Project 为 PLANNING、IMPLEMENTATION、REVISION 和 REVIEW 分别配置默认 Agent Profile。一个 Adapter 可以对应多个不同职责的 Profile。

最终 Agent 输入按固定层次组合：

```text
系统安全与运行约束                  不可编辑
内置职责提示词                      可恢复默认值
Project Instructions               项目级共享
Agent Profile Revision Instructions 面板可编辑
Topic/Task Instructions             当前对象约束
Run Instruction                     本次调用要求
Context Snapshot                    结构化上下文
```

Agent Profile Revision 至少固定：职责、Prompt Template、Model、Context Policy、Workspace Policy、Approval Policy、Timeout、Provider 和环境策略。编辑 Profile 创建新 Revision，只影响未来 Run；历史 Run 继续引用原 Revision 和最终 Prompt 快照。

模板变量使用固定白名单，不允许执行代码或读取 Secret。修改 Profile Prompt、Model 或关键 Project Instructions 后，默认创建新 Agent Session，避免旧 Session 隐藏状态与新配置混用。

权限必须由 Control Plane、Runner、Workspace Policy 和 Agent Sandbox 强制执行，不能只依赖提示词。例如 Planner 不获得可写 Workspace，Reviewer 默认只读，系统自身仍禁止自动 push。

### 8. Task 关系是显式领域数据

第一阶段支持有限关系：

```text
DERIVED_FROM
PARENT_OF
BLOCKS
RELATES_TO
SUPERSEDES
```

拆分 Task 时创建新 Task 和关系，不覆盖原 Task。关系用于追溯、搜索和 UI 展示；依赖关系是否阻止 Scheduler Claim 必须由后续明确策略决定，不从关系名称隐式推断。

## 状态影响

Task 保留执行和验收状态机，并增加 `NEEDS_CLARIFICATION`：

```text
BACKLOG → READY → RUNNING
                    ├── Agent 提问 → NEEDS_CLARIFICATION → READY（新 Run）
                    ├── 失败 ─────→ BLOCKED
                    ├── 取消 ─────→ CANCELLED
                    └── 成功 ─────→ REVIEW → ACCEPTED / CHANGES_REQUESTED
```

`READY` 表示已经存在等待 Claim 的 Run，不同时表示“需求大致明确”。需求分析和 Plan 批准由 Topic/Plan 状态表达，避免一个状态承担两种含义。

## 后果

正面影响：

- 新 Session 可以从 Topic、Plan、Decision 和 Task 重建上下文。
- 需求讨论不会污染 Task 执行状态机。
- Planner、Implementer 和 Reviewer 可以使用不同 Prompt、Model、权限和 Context Policy。
- 拆分来源、方案版本和任务关系可以完整审计。
- Label 提供灵活分类而不引入任意类型系统。

代价：

- Run、Session、Thread 和 Event 需要支持 Topic/Task 两种受约束主体。
- Plan 审批和批量创建 Task 需要新的事务与并发测试。
- UI 不再只有 Kanban，需要 Topic、Plan Review、Search 和关系视图。
- 现有 Task 中心 API、Schema 和实施顺序需要调整。

当前仓库的 Schema v8、Topic/Task 创建与查询、双主体持久讨论、评论引用 Task、Topic–Task 与对称 Task–Task 关系，以及对应 Detail UI 已实现基础纵向切片；Schema v7/v8 另增加 Task Workspace 与本地 Release，详见 ADR-0007。评论归属与关系引用保持分离，评论引用会在同一事务中建立可追溯主体关系。Plan、Clarification、Decision、Label、Task `NEEDS_CLARIFICATION` 和多主体 Run 尚未实现。后续必须继续使用版本化 migration 和回归测试迁移，不得直接修改已有 migration 或静默保留冲突语义。

## 被否决的方案

### 所有内容都是可配置 WorkItem

灵活但会引入每类型字段、状态机、权限和迁移复杂度，使产品快速演变为通用 Jira。当前选择固定 Topic、Plan、Task 语义，并用 Label 负责人类分类。

### 把需求讨论保存为 Task Comment

实现简单，但无法独立管理 Plan Revision、Clarification 和跨 Task 派生关系，也会让未明确需求过早进入执行状态机。

### Planner 自动批准并执行拆分结果

自动化程度高，但可能在范围、成本和依赖尚未确认时批量创建或执行工作。第一阶段要求人工批准 Plan，后续只能通过显式 Automation Policy 放宽。

### 通过 Prompt 约束 Agent 权限

提示词不是安全边界。文件写入、工作区、工作流命令和 Secret 访问必须由系统策略强制执行。
