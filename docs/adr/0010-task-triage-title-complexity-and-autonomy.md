# ADR-0010：Task Triage、AI 标题、复杂度与自主度

- 状态：Accepted
- 日期：2026-08-20

## 背景

Task 创建入口允许用户只描述想做什么，临时标题只是内容首行的截断，不能稳定表达目标。现有 Points 估算适合项目进度，但不能表达技术难度、需求不确定性、人工依赖和验证风险。把这些含义压成一个 Points 字段，会让调度、进度和人工判断相互污染。

## 决策

### 1. 新增固定职责 Triage Agent

Project 增加 `TRIAGE` purpose 和 `TRIAGER` Agent Profile。Triage Agent：

- 只作用于正式 Task，不作用于 Topic。
- 不获得 Task Git Workspace，不修改代码。
- 读取当前 Task、项目规则、关联讨论和已有评估。
- 产生结构化标题建议和复杂度维度评分。
- 不批准 Plan、不修改 P0–P3、不选择模型、不创建或执行实现 Task。

Worker 开启后，缺少当前有效评估的 `READY` Task 优先产生 Triage Run；同一 Task 单活跃 Run 约束仍然适用。Triage 完成后再参与普通 Implementation Claim。Triage Profile 未配置或 Triage 失败时，Task 保留临时标题并显示“复杂度未知”，但不得永久阻塞 Implementation。

### 2. 标题来源和人工锁定

Task 标题记录来源：

```text
PROVISIONAL  从用户内容首个非空行生成
AI           Triage Agent 生成
HUMAN        人工明确填写或编辑
```

人工标题默认锁定。Triage Agent 只能替换未锁定的 `PROVISIONAL` 或 `AI` 标题；人工编辑标题后创建审计事件并永久阻止后续 AI 自动覆盖。AI 标题建议使用动宾结构、表达目标，中文通常为 8–24 字；长度和非空规则由领域层强制，风格规则由 Prompt 约束。

### 3. 复杂度与工作量分离

- `Estimate Points` 表达预计工作量，用于项目进度。
- `Complexity Level` 表达实现和交付难度，不进入进度百分比分母。
- `Autonomy Level` 表达 AI 可独立完成的程度，不隐式授予权限。
- `Priority P0–P3` 仍由用户或明确领域命令决定，不从复杂度推导。

复杂度由六个 `0..4` 维度按固定权重计算：

```text
technical_complexity    25%
requirement_uncertainty 20%
change_scope            15%
validation_burden       15%
human_dependency        15%
risk_and_reversibility  10%
```

加权分数映射为：

```text
[0.0, 0.8) → C1
[0.8, 1.6) → C2
[1.6, 2.4) → C3
[2.4, 3.2) → C4
[3.2, 4.0] → C5
```

自主度由后端根据人工依赖、验证负担和不确定性确定：

```text
A3  human=0 且 validation<=1 且 uncertainty<=1
A2  human<=1，但不满足 A3
A1  human=2 或 3
A0  human=4
```

纯 AI 可完成只会降低人工依赖，不会抹去技术复杂度。复杂编译器重构即使可自动执行仍可能是 C4；简单文案需要人工确认也不应自动变成 C5。

### 4. Assessment Revision 不可变

每次评估创建不可变 `TaskAssessmentRevision`：

```text
task_id, task_assessment_version, revision
suggested_title, applied_title
six dimension scores, weighted_score
complexity_level, autonomy_level
confidence, rationale, assumptions
split_recommended, split_rationale
source_run_id, created_at
```

当前评估是最新 Revision 的投影。它记录被评估的 `task_assessment_version`；影响评估输入的 Task 需求字段变化时递增该版本，UI 必须将旧评估标成过期，不能继续当作当前结论。仅编辑或锁定标题不使复杂度评估过期。Triage 输出和外部消息均是不可信数据，必须经过结构化校验，等级和加权分数只由后端计算。

### 5. UI 与调度边界

Task 卡片紧凑显示 `C3 · A2`；详情展示六维评分、置信度、依据、假设、是否建议拆分和来源 Run。复杂度第一阶段只用于展示、筛选和拆分建议：

- 不自动修改 P0–P3。
- 不自动路由更贵模型。
- 不自动扩大 Agent 权限。
- 不因 C5 自动拆 Task；只能提出建议，后续由人或批准的 Plan 执行。

## 备选方案

### 只给 Task 增加一个可修改复杂度字段

拒绝。它无法解释来源和维度，也会覆盖历史，无法判断评估是否基于旧需求。

### 使用 Points 同时表达工作量和复杂度

拒绝。大量机械工作可能 Points 高但复杂度低；小范围安全变更可能 Points 低但风险高。

### 让复杂度直接决定优先级或模型

拒绝。复杂度不是业务紧急度，且 AI 自评不能隐式提高成本或权限。积累实际 Usage、Run 成功率和人工返工数据后再通过新 ADR 决定路由策略。

## 后果

- Task 创建后可能先发生一次低权限 Triage Run，增加少量模型调用成本。
- 用户无需填写标题，但能看到标题来源和复杂度依据。
- Scheduler 必须区分 Triage 与 Implementation 的 Task 状态副作用。
- Run Schema、Agent Profile 默认职责、Context 和结构化结果协议需要版本化迁移。
