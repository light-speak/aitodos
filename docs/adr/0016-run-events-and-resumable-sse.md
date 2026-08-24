# ADR-0016：Run Event 与可断线续传 SSE

- 状态：Accepted
- 日期：2026-08-21
- 影响：补充 ADR-0001、ADR-0012 和 ADR-0015 的实时可观测性协议

## 背景

只保存 Run 当前状态无法解释生命周期变化，也无法让页面在 Agent 执行期间及时更新。直接通过 SSE 推送 Runner 内存消息会在 Daemon 重启、浏览器断线或网络抖动后丢失事实；通过 SSE 推送完整 stdout/stderr 又会产生无界内存、网络和 UI 开销。

## 决策

### 1. 当前状态与追加事件并存

Schema v23 增加 `run_events`。每个 Run 独立维护从 1 开始的递增 `sequence`，并以 `(run_id, sequence)` 唯一约束保证顺序。事件保存稳定类型、版本化 JSON Payload 和发生时间；它是审计历史，不替代 `runs.status` 当前状态，也不采用纯 Event Sourcing。

Run Claim 和后续状态迁移必须在更新当前状态的同一个 SQLite 短事务中追加事件。已有 Run 在 migration 中生成 `RUN_IMPORTED` 当前状态快照，避免升级后 SSE 没有任何可恢复基线。

### 2. SSE 只传递有界结构化事件

端点：

```text
GET /api/runs/{runID}/events
Last-Event-ID: <sequence>
```

也支持 `?after=<sequence>`，便于新建 EventSource 时显式恢复。SSE `id` 等于 Run 内 sequence，`data` 是完整 Run Event JSON。服务端定期发送注释心跳；终态事件全部发送后结束流。

stdout、stderr、完整 Diff 和大 Artifact 不进入 SSE。事件只通知状态和小型元数据，正文继续通过受管 Artifact API 按需读取。

### 3. 前端消费必须幂等

Task 页面只为当前活跃 Run 建立一个 EventSource。客户端保存每个 Run 已消费的最大 sequence，忽略重复或倒序事件；重建连接时通过 `after` 恢复。收到终态状态事件后主动关闭连接并刷新 Run 摘要和已打开的详情。

## 后果

- 浏览器断线重连不会漏掉已经持久化的 Run 状态变化。
- 状态与审计事件不会因两个独立事务出现一边成功、一边失败。
- 实时更新不会默认传输大日志或污染 Agent Context。
- 后续取消、权限请求、Agent Session、MCP 调用和恢复事件可以复用同一有序通道，但必须分别定义领域命令和 Payload。

## 被否决的方案

### 只使用内存广播

拒绝。Daemon 重启和浏览器断线会永久丢失事件，也无法支持 `Last-Event-ID`。

### 通过 SSE 逐行发送全部日志

拒绝。日志量不可控，而且结构化生命周期事件与大文本流具有不同的读取和保留策略。
