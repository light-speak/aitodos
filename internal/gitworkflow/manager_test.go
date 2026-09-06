package gitworkflow

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/integration"
	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/workspace"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestManagerCreatesAndReusesTaskWorkspace(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	taskStore := storage.NewTaskStore(database)
	createdTask, err := taskStore.Create(ctx, task.CreateInput{Title: "实现发布面板"})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(repository, database)

	created, err := manager.CreateTaskWorkspace(ctx, createdTask.ID)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	if created.TaskID != createdTask.ID || created.BranchName == "" || created.BaseCommitSHA == "" {
		t.Fatalf("workspace = %#v", created)
	}
	if !strings.HasPrefix(created.BranchName, "aitodos/") {
		t.Fatalf("branch = %q", created.BranchName)
	}
	if created.Path != filepath.Join(repository.Paths.Worktrees, createdTask.ID) {
		t.Fatalf("path = %q", created.Path)
	}
	if head := git(t, created.Path, "rev-parse", "HEAD"); head != created.HeadSHA {
		t.Fatalf("workspace HEAD = %q, stored = %q", head, created.HeadSHA)
	}

	again, err := manager.CreateTaskWorkspace(ctx, createdTask.ID)
	if err != nil || again.ID != created.ID {
		t.Fatalf("second CreateTaskWorkspace() = %#v, %v", again, err)
	}
	updatedTask, err := taskStore.Get(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTask.CurrentWorkspaceID != created.ID || updatedTask.TargetBranch != "main" || updatedTask.BaseCommitSHA != created.BaseCommitSHA {
		t.Fatalf("updated task = %#v", updatedTask)
	}
	if updatedTask.Version != 2 {
		t.Fatalf("task version after idempotent refresh = %d, want 2", updatedTask.Version)
	}
}

func TestManagerCreatesAnnotatedReleaseTagIdempotently(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)
	input := release.CreateInput{Version: "v1.0.1", SourceBranch: "main"}

	created, err := manager.CreateRelease(ctx, input)
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if created.Status != release.StatusTagged || created.TagName != "v1.0.1" {
		t.Fatalf("release = %#v", created)
	}
	if objectType := git(t, repository.Root, "cat-file", "-t", "refs/tags/v1.0.1"); objectType != "tag" {
		t.Fatalf("tag object type = %q", objectType)
	}
	if commit := git(t, repository.Root, "rev-parse", "refs/tags/v1.0.1^{commit}"); commit != created.CommitSHA {
		t.Fatalf("tag commit = %q, release commit = %q", commit, created.CommitSHA)
	}
	again, err := manager.CreateRelease(ctx, input)
	if err != nil || again.ID != created.ID {
		t.Fatalf("second CreateRelease() = %#v, %v", again, err)
	}
	runGit(t, repository.Root, "tag", "-d", "v1.0.1")
	recovered, err := manager.CreateRelease(ctx, input)
	if err != nil || recovered.ID != created.ID {
		t.Fatalf("recovered CreateRelease() = %#v, %v", recovered, err)
	}
	if objectType := git(t, repository.Root, "cat-file", "-t", "refs/tags/v1.0.1"); objectType != "tag" {
		t.Fatalf("recovered tag object type = %q", objectType)
	}

	listed, err := manager.ListReleases(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("ListReleases() = %#v, %v", listed, err)
	}
}

func TestManagerReportsRepositoryBranchesAndHead(t *testing.T) {
	repository, database := initializeRepository(t)
	runGit(t, repository.Root, "remote", "add", "origin", "https://secret-token@example.com/team/repo.git?token=hidden")
	runGit(t, repository.Root, "update-ref", "refs/remotes/origin/main", "refs/heads/main")
	runGit(t, repository.Root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	runGit(t, repository.Root, "branch", "--set-upstream-to=origin/main", "main")
	if err := os.WriteFile(filepath.Join(repository.Root, "ahead.txt"), []byte("ahead\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository.Root, "add", "ahead.txt")
	runGit(t, repository.Root, "commit", "--quiet", "-m", "ahead")
	manager := New(repository, database)
	info, err := manager.RepositoryInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.CurrentBranch != "main" || info.DefaultBranch != "main" || info.HeadSHA == "" || len(info.Branches) != 1 || info.Branches[0].Name != "main" {
		t.Fatalf("repository info = %#v", info)
	}
	if info.RemoteDefaultBranch != "main" || info.Upstream != "origin/main" || info.Ahead == nil || *info.Ahead != 1 || info.Behind == nil || *info.Behind != 0 {
		t.Fatalf("tracking info = %#v", info)
	}
	if info.Root != repository.Root || info.GitCommonDir != repository.GitCommonDir || info.GitVersion == "" {
		t.Fatalf("repository identity = %#v", info)
	}
	if info.UserName != "AiTodos Test" || info.UserEmail != "aitodos@example.invalid" {
		t.Fatalf("git identity = %#v", info)
	}
	if len(info.Remotes) != 1 || info.Remotes[0].FetchURL != "https://example.com/team/repo.git" || info.Remotes[0].PushURL != "https://example.com/team/repo.git" {
		t.Fatalf("sanitized remotes = %#v", info.Remotes)
	}
}

func TestManagerUpdatesTargetBranchBeforeWorkspace(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	runGit(t, repository.Root, "branch", "release/macos")
	store := storage.NewTaskStore(database)
	created, err := store.Create(ctx, task.CreateInput{Title: "macOS 发布", TargetBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(repository, database)

	updated, err := manager.UpdateTaskTargetBranch(ctx, created.ID, created.Version, "release/macos")
	if err != nil || updated.TargetBranch != "release/macos" {
		t.Fatalf("UpdateTaskTargetBranch() = %#v, %v", updated, err)
	}
	if _, err := manager.UpdateTaskTargetBranch(ctx, created.ID, updated.Version, "missing"); err == nil {
		t.Fatal("missing target branch unexpectedly accepted")
	}
	if _, err := manager.CreateTaskWorkspace(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	updated, err = store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpdateTaskTargetBranch(ctx, created.ID, updated.Version, "main"); !errors.Is(err, storage.ErrTaskWorkspaceExists) {
		t.Fatalf("UpdateTaskTargetBranch() after workspace error = %v", err)
	}
}

func TestManagerReportsRepositoryWithoutInitialCommit(t *testing.T) {
	repository, database := initializeUnbornRepository(t)
	ctx := context.Background()
	manager := New(repository, database)
	info, err := manager.RepositoryInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.HasHead || info.HeadSHA != "" || info.CurrentBranch != "main" || len(info.Branches) != 0 {
		t.Fatalf("repository info = %#v", info)
	}
	created, err := storage.NewTaskStore(database).Create(ctx, task.CreateInput{Title: "等待首个 Commit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateTaskWorkspace(ctx, created.ID); !errors.Is(err, ErrRepositoryUnborn) {
		t.Fatalf("CreateTaskWorkspace() error = %v, want ErrRepositoryUnborn", err)
	}
}

func TestManagerSummarizesAndLoadsTaskChanges(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	createdTask, err := storage.NewTaskStore(database).Create(ctx, task.CreateInput{Title: "展示修改"})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(repository, database)
	createdWorkspace, err := manager.CreateTaskWorkspace(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(createdWorkspace.Path, "README.md"), []byte("# changed\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(createdWorkspace.Path, "new.txt"), []byte("new file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	changes, err := manager.TaskChanges(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changes.FileCount != 2 || changes.Additions != 3 || changes.Deletions != 1 {
		t.Fatalf("changes = %#v", changes)
	}
	patch, err := manager.TaskFileDiff(ctx, createdTask.ID, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch.Patch, "+# changed") || !strings.Contains(patch.Patch, "-# test") {
		t.Fatalf("patch = %q", patch.Patch)
	}
}

func TestManagerCompletesManualReviewFlow(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	store := storage.NewTaskStore(database)
	created, err := store.Create(ctx, task.CreateInput{Title: "走完人工验收"})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(repository, database)
	createdWorkspace, err := manager.CreateTaskWorkspace(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	worktree := &createdWorkspace
	if err := os.WriteFile(filepath.Join(worktree.Path, "README.md"), []byte("# reviewed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err = manager.SubmitTaskReview(ctx, created.ID, created.Version)
	if err != nil || created.Status != task.StatusReview {
		t.Fatalf("SubmitTaskReview() = %#v, %v", created, err)
	}
	updated, review, err := manager.ReviewTask(ctx, created.ID, created.Version, task.ReviewInput{Decision: task.ReviewAccepted})
	if err != nil || updated.Status != task.StatusAccepted || review.CommitSHA == worktree.BaseCommitSHA {
		t.Fatalf("ReviewTask() = %#v, %#v, %v", updated, review, err)
	}
	committed, err := manager.TaskWorkspace(ctx, created.ID)
	if err != nil || committed == nil || committed.Dirty || review.CommitSHA != committed.HeadSHA {
		t.Fatalf("TaskWorkspace() after review = %#v, %v", committed, err)
	}
	released, err := manager.CreateRelease(ctx, release.CreateInput{Version: "1.2.0", SourceBranch: committed.BranchName})
	if err != nil || len(released.TaskIDs) != 1 || released.TaskIDs[0] != created.ID {
		t.Fatalf("CreateRelease() = %#v, %v", released, err)
	}
}

func TestManagerDoesNotCommitWorkspaceWhenRequiredTestsAreMissing(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	store := storage.NewTaskStore(database)
	created, err := store.Create(ctx, task.CreateInput{Title: "测试不足时拒绝验收"})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(repository, database)
	createdWorkspace, err := manager.CreateTaskWorkspace(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(createdWorkspace.Path, "unverified.txt"), []byte("pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err = manager.SubmitTaskReview(ctx, created.ID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.NewQualityStore(database).CreateTestCase(ctx, created.ID, quality.TestCaseInput{
		Title: "必须验证", Required: true, CreatedBy: quality.TestCreatorHuman,
	}); err != nil {
		t.Fatal(err)
	}

	headBefore := git(t, createdWorkspace.Path, "rev-parse", "HEAD")
	if _, _, err := manager.ReviewTask(ctx, created.ID, created.Version, task.ReviewInput{
		Decision: task.ReviewAccepted,
	}); !errors.Is(err, storage.ErrRequiredTestsNotPassed) {
		t.Fatalf("ReviewTask() error = %v", err)
	}
	currentWorkspace, err := manager.TaskWorkspace(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentWorkspace == nil || !currentWorkspace.Dirty || currentWorkspace.HeadSHA != headBefore {
		t.Fatalf("workspace changed after rejected review = %#v", currentWorkspace)
	}
}

func TestManagerIntegratesAcceptedTaskFastForward(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)
	accepted, review, taskWorkspace := createAcceptedTask(t, manager, database, "集成已验收任务", "feature.txt", "ready\n")
	if _, _, err := manager.SyncTaskTarget(ctx, accepted.ID); !errors.Is(err, ErrTargetSyncNotNeeded) {
		t.Fatalf("SyncTaskTarget() before divergence error = %v", err)
	}

	result, err := manager.IntegrateTask(ctx, accepted.ID)
	if err != nil {
		t.Fatalf("IntegrateTask() error = %v", err)
	}
	if result.Status != integration.StatusSucceeded || result.SourceCommitSHA != review.CommitSHA || result.TargetAfterSHA != review.CommitSHA {
		t.Fatalf("integration result = %#v", result)
	}
	if head := git(t, repository.Root, "rev-parse", "main"); head != review.CommitSHA {
		t.Fatalf("main HEAD = %q, want %q", head, review.CommitSHA)
	}
	if taskWorkspace.HeadSHA != review.CommitSHA {
		t.Fatalf("workspace HEAD = %q, review = %q", taskWorkspace.HeadSHA, review.CommitSHA)
	}

	again, err := manager.IntegrateTask(ctx, accepted.ID)
	if err != nil || again.ID != result.ID {
		t.Fatalf("idempotent IntegrateTask() = %#v, %v", again, err)
	}
}

func TestManagerSyncsDivergedTargetAndRequiresRevision(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)
	accepted, _, _ := createAcceptedTask(t, manager, database, "同步目标分支", "feature.txt", "task\n")
	qualityStore := storage.NewQualityStore(database)
	testCase, err := qualityStore.CreateTestCase(ctx, accepted.ID, quality.TestCaseInput{
		Title: "回归检查", Required: true, CreatedBy: quality.TestCreatorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualityStore.AddTestResult(ctx, accepted.ID, testCase.ID, quality.TestResultInput{
		Outcome: quality.OutcomePassed, EvidenceKind: quality.EvidenceHuman, Summary: "同步前验证通过",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.Root, "target.txt"), []byte("target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository.Root, "add", "target.txt")
	runGit(t, repository.Root, "commit", "--quiet", "-m", "advance target")

	blocked, err := manager.IntegrateTask(ctx, accepted.ID)
	if !errors.Is(err, ErrTargetNeedsSync) || blocked.Status != integration.StatusNeedsSync {
		t.Fatalf("IntegrateTask() = %#v, %v", blocked, err)
	}
	syncedTask, synced, err := manager.SyncTaskTarget(ctx, accepted.ID)
	if err != nil {
		t.Fatalf("SyncTaskTarget() error = %v", err)
	}
	if syncedTask.Status != task.StatusChangesRequested || synced.Status != integration.StatusSynced {
		t.Fatalf("sync result = %#v, %#v", syncedTask, synced)
	}
	currentWorkspace, err := manager.TaskWorkspace(ctx, accepted.ID)
	if err != nil || currentWorkspace == nil || currentWorkspace.Dirty {
		t.Fatalf("workspace after sync = %#v, %v", currentWorkspace, err)
	}
	if _, err := os.Stat(filepath.Join(currentWorkspace.Path, "feature.txt")); err != nil {
		t.Fatalf("task change missing after sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(currentWorkspace.Path, "target.txt")); err != nil {
		t.Fatalf("target change missing after sync: %v", err)
	}
	store := storage.NewTaskStore(database)
	running, err := store.ApplyCommand(ctx, syncedTask.ID, syncedTask.Version, task.CommandClaimRun)
	if err != nil {
		t.Fatal(err)
	}
	reviewing, err := store.ApplyCommand(ctx, running.ID, running.Version, task.CommandRunSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.ReviewTask(ctx, reviewing.ID, reviewing.Version, task.ReviewInput{Decision: task.ReviewAccepted}); !errors.Is(err, storage.ErrRequiredTestsNotPassed) {
		t.Fatalf("stale test evidence review error = %v", err)
	}
}

func TestManagerAbortsSyncConflictBeforeRevision(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)
	accepted, _, _ := createAcceptedTask(t, manager, database, "处理同步冲突", "README.md", "task version\n")
	if err := os.WriteFile(filepath.Join(repository.Root, "README.md"), []byte("target version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository.Root, "add", "README.md")
	runGit(t, repository.Root, "commit", "--quiet", "-m", "conflicting target")

	syncedTask, attempt, err := manager.SyncTaskTarget(ctx, accepted.ID)
	if err != nil {
		t.Fatalf("SyncTaskTarget() error = %v", err)
	}
	if syncedTask.Status != task.StatusChangesRequested || attempt.Status != integration.StatusConflict {
		t.Fatalf("conflict sync = %#v, %#v", syncedTask, attempt)
	}
	currentWorkspace, err := manager.TaskWorkspace(ctx, accepted.ID)
	if err != nil || currentWorkspace == nil || currentWorkspace.Dirty {
		t.Fatalf("workspace after conflict = %#v, %v", currentWorkspace, err)
	}
	if gitPath := git(t, currentWorkspace.Path, "rev-parse", "--git-path", "MERGE_HEAD"); fileExists(gitPath) {
		t.Fatalf("MERGE_HEAD still exists at %q", gitPath)
	}
}

func TestManagerRecoversCompletedAndInterruptedIntegrations(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)

	completedTask, completedReview, completedWorkspace := createAcceptedTask(t, manager, database, "恢复已完成集成", "completed.txt", "done\n")
	targetBefore := git(t, repository.Root, "rev-parse", "main")
	completedAttempt, err := manager.integrations.Reserve(ctx, integration.ReserveInput{
		TaskID: completedTask.ID, ReviewID: completedReview.ID, Operation: integration.OperationIntegrate,
		TargetBranch: "main", SourceCommitSHA: completedReview.CommitSHA, TargetBeforeSHA: targetBefore,
	})
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repository.Root, "merge", "--ff-only", completedReview.CommitSHA)

	interruptedTask, interruptedReview, _ := createAcceptedTask(t, manager, database, "恢复中断集成", "interrupted.txt", "pending\n")
	interruptedAttempt, err := manager.integrations.Reserve(ctx, integration.ReserveInput{
		TaskID: interruptedTask.ID, ReviewID: interruptedReview.ID, Operation: integration.OperationIntegrate,
		TargetBranch: "main", SourceCommitSHA: interruptedReview.CommitSHA, TargetBeforeSHA: completedReview.CommitSHA,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.RecoverIntegrations(ctx); err != nil {
		t.Fatal(err)
	}
	completed, err := manager.integrations.Get(ctx, completedAttempt.ID)
	if err != nil || completed.Status != integration.StatusSucceeded || completed.TargetAfterSHA != completedReview.CommitSHA {
		t.Fatalf("completed recovery = %#v, %v", completed, err)
	}
	if completedWorkspace.HeadSHA != completedReview.CommitSHA {
		t.Fatalf("completed workspace = %#v", completedWorkspace)
	}
	interrupted, err := manager.integrations.Get(ctx, interruptedAttempt.ID)
	if err != nil || interrupted.Status != integration.StatusFailed || interrupted.FailureKind != "INTERRUPTED" {
		t.Fatalf("interrupted recovery = %#v, %v", interrupted, err)
	}
}

func TestManagerRecoversCompletedTargetSync(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)
	accepted, review, item := createAcceptedTask(t, manager, database, "恢复目标同步", "feature.txt", "task\n")
	if err := os.WriteFile(filepath.Join(repository.Root, "target.txt"), []byte("target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository.Root, "add", "target.txt")
	runGit(t, repository.Root, "commit", "--quiet", "-m", "advance target for recovery")
	targetSHA := git(t, repository.Root, "rev-parse", "main")
	attempt, err := manager.integrations.Reserve(ctx, integration.ReserveInput{
		TaskID: accepted.ID, ReviewID: review.ID, Operation: integration.OperationSync,
		TargetBranch: "main", SourceCommitSHA: review.CommitSHA, TargetBeforeSHA: targetSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, item.Path, "merge", "--no-edit", targetSHA)

	if err := manager.RecoverIntegrations(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := manager.integrations.Get(ctx, attempt.ID)
	if err != nil || recovered.Status != integration.StatusSynced || recovered.WorkspaceAfterSHA == review.CommitSHA {
		t.Fatalf("sync recovery = %#v, %v", recovered, err)
	}
	updated, err := storage.NewTaskStore(database).Get(ctx, accepted.ID)
	if err != nil || updated.Status != task.StatusChangesRequested {
		t.Fatalf("task after recovery = %#v, %v", updated, err)
	}
}

func TestManagerRecordsAlreadyContainedIntegrationAndQueriesIt(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	manager := New(repository, database)
	accepted, review, _ := createAcceptedTask(t, manager, database, "记录已包含提交", "contained.txt", "contained\n")
	if latest, err := manager.TaskIntegration(ctx, accepted.ID); err != nil || latest != nil {
		t.Fatalf("initial TaskIntegration() = %#v, %v", latest, err)
	}
	runGit(t, repository.Root, "merge", "--ff-only", review.CommitSHA)
	result, err := manager.IntegrateTask(ctx, accepted.ID)
	if err != nil || result.Status != integration.StatusSucceeded || result.TargetAfterSHA != review.CommitSHA {
		t.Fatalf("IntegrateTask() = %#v, %v", result, err)
	}
	latest, err := manager.TaskIntegration(ctx, accepted.ID)
	if err != nil || latest == nil || latest.ID != result.ID {
		t.Fatalf("TaskIntegration() = %#v, %v", latest, err)
	}
	reviews, err := manager.ListTaskReviews(ctx, accepted.ID)
	if err != nil || len(reviews) != 1 || reviews[0].CommitSHA != review.CommitSHA {
		t.Fatalf("ListTaskReviews() = %#v, %v", reviews, err)
	}
}

func createAcceptedTask(
	t *testing.T,
	manager *Manager,
	database *sql.DB,
	title string,
	path string,
	content string,
) (task.Task, task.Review, workspace.Workspace) {
	t.Helper()
	ctx := context.Background()
	store := storage.NewTaskStore(database)
	created, err := store.Create(ctx, task.CreateInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	taskWorkspace, err := manager.CreateTaskWorkspace(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskWorkspace.Path, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err = manager.SubmitTaskReview(ctx, created.ID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	accepted, review, err := manager.ReviewTask(ctx, created.ID, created.Version, task.ReviewInput{Decision: task.ReviewAccepted})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := manager.TaskWorkspace(ctx, created.ID)
	if err != nil || refreshed == nil {
		t.Fatalf("TaskWorkspace() = %#v, %v", refreshed, err)
	}
	return accepted, review, *refreshed
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestManagerRejectsConflictingExistingTagAndRecordsFailure(t *testing.T) {
	repository, database := initializeRepository(t)
	runGit(t, repository.Root, "tag", "v2.0.0")
	manager := New(repository, database)

	_, err := manager.CreateRelease(context.Background(), release.CreateInput{Version: "2.0.0", SourceBranch: "main"})
	if !errors.Is(err, ErrTagConflict) {
		t.Fatalf("CreateRelease() error = %v, want ErrTagConflict", err)
	}
	releases, err := manager.ListReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].Status != release.StatusFailed {
		t.Fatalf("releases = %#v", releases)
	}
}

func TestManagerQuarantinesWorkspaceSymlinkOutsideManagedRoot(t *testing.T) {
	ctx := context.Background()
	repository, database := initializeRepository(t)
	taskStore := storage.NewTaskStore(database)
	createdTask, err := taskStore.Create(ctx, task.CreateInput{Title: "校验工作区路径"})
	if err != nil {
		t.Fatal(err)
	}
	branchName := taskBranchName(repository.Config.Name, createdTask.Key, createdTask.ID)
	outsidePath := filepath.Join(filepath.Dir(repository.Root), filepath.Base(repository.Root)+"-outside")
	t.Cleanup(func() { _ = os.RemoveAll(outsidePath) })
	runGit(t, repository.Root, "worktree", "add", "--quiet", "-b", branchName, outsidePath, "main")
	managedPath := filepath.Join(repository.Paths.Worktrees, createdTask.ID)
	if err := os.Symlink(outsidePath, managedPath); err != nil {
		t.Fatal(err)
	}

	manager := New(repository, database)
	_, err = manager.CreateTaskWorkspace(ctx, createdTask.ID)
	if !errors.Is(err, ErrWorkspaceIdentity) {
		t.Fatalf("CreateTaskWorkspace() error = %v, want ErrWorkspaceIdentity", err)
	}
	stored, err := storage.NewWorkspaceStore(database).GetByTask(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != workspace.StateQuarantined {
		t.Fatalf("workspace state = %q, want %q", stored.State, workspace.StateQuarantined)
	}
}

func initializeRepository(t *testing.T) (*project.Project, *sql.DB) {
	t.Helper()
	temporaryRoot, err := filepath.Abs(filepath.Join("..", "..", ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := os.MkdirTemp(temporaryRoot, "gitworkflow-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repoRoot) })
	runGit(t, repoRoot, "init", "--quiet", "--initial-branch=main")
	runGit(t, repoRoot, "config", "user.name", "AiTodos Test")
	runGit(t, repoRoot, "config", "user.email", "aitodos@example.invalid")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "--quiet", "-m", "initial")
	repository, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "add", ".ats/.gitignore", ".ats/project.toml")
	runGit(t, repoRoot, "commit", "--quiet", "-m", "configure aitodos")
	database, err := storage.OpenExisting(context.Background(), repository.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return repository, database
}

func initializeUnbornRepository(t *testing.T) (*project.Project, *sql.DB) {
	t.Helper()
	temporaryRoot, err := filepath.Abs(filepath.Join("..", "..", ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := os.MkdirTemp(temporaryRoot, "gitworkflow-unborn-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repoRoot) })
	runGit(t, repoRoot, "init", "--quiet", "--initial-branch=main")
	repository, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), repository.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return repository, database
}

func git(t *testing.T, directory string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	_ = git(t, directory, args...)
}
