# ADR-0009：AI 估算进度、Task 测试项与可验证证据

- 状态：Accepted
- 日期：2026-08-20

## 背景

只统计 Task 数量会把“一行修复”和“大型重构”视为相同工作量；直接让 AI 给出一个项目完成百分比又容易制造虚假精确度。测试也不能只显示 Agent 声称“已通过”，否则人无法区分真实命令结果、人工验证和模型判断。

## 决策

### 1. 估算是版本化事实，不覆盖 Task

每次估算创建不可变 `Estimate Revision`，至少保存：

```text
task_id, revision
points, remaining_points
confidence, rationale
source, source_run_id, created_at
```

工作量使用 `1 / 2 / 3 / 5 / 8 / 13` Points。`remaining_points` 必须在 `0..points` 范围内。置信度使用 `0..1`，UI 可将它转换成人类可读等级。AI、人工均可创建新 Revision，但 UI 必须显示来源；旧 Revision 不删除。

MVP 以最新 Revision 作为当前预测；输入 Revision 和 stale 检测属于后续扩展，在实现前不得声称估算已自动失效。

### 2. 同时展示严格进度与 AI 预测

整体进度页至少显示两个不同指标：

```text
严格进度 = 已 ACCEPTED Task 数 / 非 CANCELLED Task 数
AI 预测进度 = Σ(points - remaining_points) / Σ(points)
估算覆盖率 = 已估算的非 CANCELLED Task 数 / 非 CANCELLED Task 数
```

没有有效估算时显示“未知”，不得显示 0%。未估算 Task 不进入分母，并必须同时展示覆盖率，避免进度被选择性估算扭曲。AI 预测必须附置信度分布、更新时间、主要假设和阻塞项，不能伪装成精确承诺。

### 3. Test Case 与 Test Result 分离

Task 拥有结构化 Test Case：

```text
id, task_id, title, description
required, sort_order
created_by, source_run_id
created_at, updated_at
```

每次测试产生不可变 Test Result：

```text
test_case_id, task_id, source_run_id
outcome, evidence_kind
command, artifact_ref, summary, created_at
```

状态为 `PASSED`、`FAILED` 或 `BLOCKED`。证据来源为：

- `COMMAND`：Runner 实际执行并记录退出码和 Artifact。
- `HUMAN`：人工确认并记录说明。
- `AGENT_REPORT`：仅有 Agent 结构化报告，不能伪装成已验证命令。

### 4. 验收门槛

存在 required Test Case 时，正常 `AcceptTask` 要求每项最新结果为 `PASSED`，且证据为 `COMMAND` 或 `HUMAN`。MVP 不提供 Override；`AGENT_REPORT` 固定不满足门槛。

### 5. Progress Projection 可重建

整体进度、测试通过率、状态分布、阻塞列表和 Worker 活动是可重建只读投影，不是唯一事实来源。它从 Task、Estimate Revision、Test Case、Test Result、Run 和 Review 计算，不允许直接 PATCH 百分比。

## 整体进度页面

MVP 已包含：

- 按 Task 数计算的严格验收进度。
- 按 Points 计算的 AI 预测、剩余点数和估算覆盖率。
- required Test Case 的已验证通过数与仅 Agent 自报数。
- Task 详情中的估算依据、置信度、来源和测试证据。

状态分布、并发占用、最近 Run、逐 Task 进度表和阻塞聚合属于后续增强，未实现前不得写入当前能力清单。

## 备选方案

### 按 Task 数量直接计算百分比

拒绝。不同 Task 工作量差异过大，结果误导。

### 只显示一个 AI 百分比

拒绝。无法解释来源、覆盖率、置信度和实际验收情况。

### Agent 文本中出现“测试通过”就标绿

拒绝。自然语言声明不是可验证证据，必须明确标注 `AGENT_REPORT`。

## 后果

- 项目进度具有解释性，但仍是估算而非承诺。
- Runner 必须把测试命令、退出码和日志 Artifact 结构化关联到 Test Case。
- Planner 可提出 Test Case 和初始 Estimate 草案；正式写入仍遵循 Plan 批准或显式人工命令。
- Reviewer Context 优先读取 required Test Case、最新 Test Result 和失败证据，不需要重放全部日志。
