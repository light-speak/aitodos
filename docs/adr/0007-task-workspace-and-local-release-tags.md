# ADR-0007：Task 长期 Workspace 与本地 Release Tag

- 状态：Accepted
- 日期：2026-08-19

## 背景

Task 的数据库 `version` 是乐观锁修订号，不是软件版本。若 UI 将它显示为 `v1`，会与用户理解的 `v1.0.1` Release 混淆。同时，Agent 修改需要明确隔离在 Task worktree 中，发布记录需要能回答“哪个版本对应哪个不可变 Commit”，而不能只记录会继续移动的分支名。

## 决策

1. 一个 Task 显式创建并长期复用一个 linked worktree 和一个 Task Branch：

   ```text
   path:   <repo>/.ats/worktrees/<task-id>
   branch: aitodos/<project-slug>/<task-key>-<short-task-id>
   ```

2. Workspace 创建前将目标本地分支解析为固定 Base SHA。每次读取都重新校验受管路径、`gitCommonDir`、Branch、HEAD 和 dirty 状态。身份不一致时标记 `QUARANTINED`，不自动 reset、checkout 或删除。
3. 共享 Git 元数据操作使用项目 `.ats/runtime/git.lock` 串行化；Git 调用使用 executable 与 argv，不拼接 shell command。
4. UI 不展示 Topic/Task 的内部 `version`。Task 详情展示工作分支、HEAD、目标分支、Base SHA、dirty 状态和受管路径。
5. Release 使用 SemVer。规范 Tag 名为 `v<semver>`，并固定保存来源本地分支与创建时解析出的完整 Commit SHA。
6. Release 创建采用可恢复流程：先以 `CREATING` 固定数据库事实，再创建本地 annotated tag，验证成功后标记 `TAGGED`；失败保存 `FAILED`，相同输入可以幂等重放。
7. 已存在的同名 Tag 只有在它是 annotated tag 且指向相同 Commit 时才视为幂等成功，否则返回冲突。
8. Release 只包含来源分支 Commit 中已经提交的内容。Task Workspace 中未提交或尚未合入来源分支的修改不会进入 Release。
9. 系统不自动 push，不自动 merge，不因创建 Release 自动 commit。
10. Agent 默认不得 commit。未来若实现提交，由用户触发“验收并提交”领域命令，Control Plane 在校验 Workspace 和 Review 后创建系统可审计 Commit；该交互需在实现 AcceptTask 前细化，但不得改成 Agent 静默提交。

## 备选方案

### 直接使用 Task `version` 作为发布版本

拒绝。乐观锁修订号会因领域更新增长，不具备 SemVer 和 Git 对应关系。

### 每个 Run 新建 worktree

拒绝。驳回后的连续修改、磁盘占用和清理复杂度更差；Task 单活跃 Run 与 Workspace Lease 已提供顺序复用边界。

### 只保存分支名，不保存 Commit

拒绝。分支可移动，无法形成可审计发布事实。

### 创建 lightweight tag

拒绝。annotated tag 是独立 Git 对象，可验证类型并保存发布说明，更适合作为本地 Release 边界。

### 自动 push Tag 或自动 merge Task Branch

拒绝。它们会改变远端或目标分支，超出本地单用户 MVP 的安全边界，也不能从“创建版本”隐式推导授权。

## 后果

- 用户能从 Task 详情看到真实 Git 工作身份，从 Release 面板看到版本、来源分支和固定 Commit。
- 创建 Release 前必须确保目标内容已经提交到所选来源分支，并配置本地 Git 用户信息以创建 annotated tag。
- Release 与可选 Task 关联只用于审计，当前不证明 Task 修改已经合入 Commit。
- Workspace/Release 数据使用 Schema v7/v8 持久化；Git 操作失败不会丢失恢复所需事实。
