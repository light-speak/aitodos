# ADR-0018：Run 取消、Finalization Intent 与 Crash Recovery

- 状态：Accepted
- 日期：2026-08-21
- 影响：Scheduler、Runner、SQLite、Workspace 和 Daemon 启动顺序

## 背景

Runner、Agent 子进程、SQLite 和 Git Workspace 无法形成单个 exactly-once 事务。Daemon 或 Runner 可能在 Agent 已修改文件、日志已写入但 Run 尚未进入终态时崩溃。仅凭数据库 `RUNNING` 或 PID 会误判；自动重试又可能再次调用 AI、重复消耗 Token 并覆盖已有修改。

## 决策

1. Schema v24 将取消建模为独立意图。Runner 每 500 ms 检查取消，终止整个 Agent 进程组，保存日志和 Workspace 快照后才写 `CANCELLED`。Task 进入 `BLOCKED`，由人类显式重新排队。
2. Run 提供有界分页查询、Purpose/Status/主体筛选、详情、按需日志、取消和 Task Retry 命令。Retry 不覆盖历史 Run。
3. Schema v27 保存 `runner_identity`、`lease_heartbeat_at` 和不可变 `run_finalization_intents`。Runner 身份由 PID、内核进程启动时间构成，并在 CLI 启动时校验不可预测 Run nonce；禁止降级为仅 PID。
4. Runner 默认每 15 秒续租 45 秒 Lease。续租失败会取消当前 Agent 上下文，不允许失去 fencing token 的 Runner继续无界运行。
5. Agent 退出和结果解析后，Runner 先原子冻结终态、失败信息和可选 Clarification，再进入 `FINALIZING`；随后采集 Workspace，最后幂等提交 Task/Run 状态和事件。
6. Daemon 在启动 Scheduler 前扫描旧非终态 Run。身份匹配的旧 Runner继续运行并被观察；已死亡的 `FINALIZING` Run 重放 Workspace 收尾和冻结终态；其他不明确 Run 标记 `LOST` 并阻塞 Task。
7. Recovery 不调用 Agent、不自动创建 Retry Run、不 reset/checkout Dirty Workspace。Workspace 身份异常沿用 Git Workflow 的 `QUARANTINED` 规则。
8. 同一 Daemon 观察到 Runner 退出时也优先重放已冻结 Finalization；无法安全完成时才标记 `LOST`。

## 后果

- Daemon 重启不会仅因 Lease 过期重复调用 AI。
- Clarification、成功、失败、超时和取消都可以从冻结事实恢复终态。
- 人类能在 Run 历史中看到 `LOST`，检查 Workspace 后再决定是否重试。
- 当前恢复覆盖 Runner、Agent 进程组、Run/Task 状态、Approval、Artifact 索引和 Git Workspace。MCP 浏览器资源的可验证关闭仍依赖 ADR-0012 的受管资源租约，未实现前不得宣称可以回收未知外部浏览器。

## 未选择方案

### Lease 过期后直接重新执行

拒绝。Lease 只能证明心跳缺失，不能证明 Agent 从未启动。

### Daemon 重启时终止所有匹配 PID

拒绝。旧 Runner 可能仍在正常执行，强制停止会丢失有效工作。
