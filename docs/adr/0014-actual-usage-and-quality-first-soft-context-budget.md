# ADR-0014：真实 Usage 与质量优先的软 Context Budget

- 状态：Accepted
- 日期：2026-08-21
- 影响：修订 ADR-0004 和 ADR-0011 中本地 Token 硬预算的执行语义

## 背景

本地 Token 估算无法等同于 Provider 实际 Tokenizer，也无法覆盖 Agent CLI 自动加入的全局指令、Tool 定义、内部多次模型请求和 Session 历史。将 `max_input_tokens - reserved_output_tokens` 作为硬限制，会让长任务在调用模型前失败，或为了节省成本省略影响正确性的上下文。

另一方面，只保留 Run 累计输入也无法判断单次请求是否接近模型 Context Window。缓存输入仍包含在输入中并占用上下文，只是可能减少重复计算和费用。系统必须把估算、累计 Usage 和单次峰值分开建模。

## 决策

### 1. 质量优先，预算只作软保护

Context Builder 继续进行稳定哈希去重，并用本地估算控制低优先级历史、检索摘要和可选 Artifact 片段。以下必需内容不因本地估算超限而裁剪或拒绝执行：

- 系统安全规则和职责 Prompt。
- 当前 Task、Acceptance Criteria 和 Workspace 身份。
- Project Instructions、批准 Plan、有效 Decision 和开放 Clarification。
- 当前 Revision 所依据的驳回意见和机器结果契约。
- Profile 明确标记 required 的有效 Skill 内容。

必需内容超过软目标时，Prompt 仍完整生成，Context Manifest 记录 `required_over_budget=true`。这不是 Provider Context Window 保证；未来只有 Adapter Probe 能可靠提供模型上限时，才增加调用前的精确校验。

### 2. 估算只用于诊断，不冒充实际用量

Context Manifest 保留来源、哈希、采用结果、省略原因和 `estimated_tokens`，用于解释 Context Builder 的取舍。普通 Agent 配置界面不展示输入预算和输出预留，避免用户把它们误认为计费上限或 Task 长度限制。

### 3. 持久化 Adapter 提供的真实 Usage

Schema v20 新增一对一 `run_usage`：

```text
run_id
input_tokens, cached_input_tokens, cache_write_input_tokens
output_tokens, reasoning_output_tokens
model_requests, peak_input_tokens
source, captured_at
```

所有指标可空，未知不写零。旧 Codex Adapter 从最后一个有效 `turn.completed` JSONL 事件读取累计 Usage。Codex App Server Adapter 从 `thread/tokenUsage/updated` 读取当前 `turnId` 的结构化 Usage：按累计快照去重后累加每次请求的 `last`，从而得到当前 Run 的累计值，并记录请求数和单次输入峰值。Resume 时其他 `turnId` 的历史快照不会计入新 Run。未知、破损、负值和未来事件不导致 Run 失败，也不会生成伪造统计。Usage 在 Agent 退出后、Run Finalization 前幂等保存，因此成功、失败、取消或超时只要事件可用都可记录。

### 4. 汇总口径明确区分累计值与单次上下文

项目统计展示：

- Run 总数和已采集 Usage 的 Run 数。
- 累计输入、缓存输入、成对可计算的非缓存输入和缓存命中率。
- 累计输出、推理输出和按 Run Purpose 汇总。
- Adapter 可用时的模型请求次数与单次输入峰值。

`cached_input_tokens` 是 `input_tokens` 的子集。累计输入是一个 Run 内多次请求之和，不标注为“Context 大小”。不能从累计输入推断单次峰值或请求次数。Cost 缺少价格快照、币种和来源时不估算。

## 后果

- 长任务不会因本地近似 Token 数而丢失关键事实或在调用前失败。
- 仍能通过去重、近期工作集和按需检索减少明显无效上下文。
- 人类可以按职责比较真实用量和缓存效果，而不会把缓存或累计输入误解为免费上下文。
- 未提供结构化 Usage 的 Generic Adapter 保持未知，统计页通过采集覆盖率显式展示缺口。

## 被否决的方案

### 完全删除 Context 估算

拒绝。没有软目标就无法对低价值历史和检索结果做稳定、有审计记录的取舍，也无法提前发现 Context 异常膨胀。

### 继续让用户配置硬 Token 预算

拒绝。用户通常不知道 Agent CLI 的隐式上下文和模型实际窗口，硬限制容易损害结果，也会把预算误解为成本上限。

### 用 Run 累计输入估算单次 Context 峰值

拒绝。一个 Run 可以包含多次模型请求，累计值不能证明任何一次请求的大小。
