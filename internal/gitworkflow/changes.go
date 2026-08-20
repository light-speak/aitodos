package gitworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrChangeNotFound 表示请求的路径不属于当前 Task 变更清单。
var ErrChangeNotFound = errors.New("task change not found")

const maxPatchBytes = 512 << 10

// ChangedFile 是相对 Task Base Commit 的单个文件变更摘要。
type ChangedFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
}

// TaskChanges 是 Task Workspace 的有界变更摘要。
type TaskChanges struct {
	BaseCommitSHA string        `json:"base_commit_sha"`
	HeadSHA       string        `json:"head_sha"`
	Dirty         bool          `json:"dirty"`
	FileCount     int           `json:"file_count"`
	Additions     int           `json:"additions"`
	Deletions     int           `json:"deletions"`
	Files         []ChangedFile `json:"files"`
}

// FileDiff 是按需读取且可能被截断的单文件 Unified Diff。
type FileDiff struct {
	Path      string `json:"path"`
	Patch     string `json:"patch"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
}

// TaskChanges 计算 Task Workspace 相对 Base Commit 的文件变更。
func (manager *Manager) TaskChanges(ctx context.Context, taskID string) (TaskChanges, error) {
	item, err := manager.TaskWorkspace(ctx, taskID)
	if err != nil || item == nil {
		return TaskChanges{}, err
	}
	output, err := manager.gitOutput(ctx, item.Path, "diff", "--numstat", item.BaseCommitSHA, "--")
	if err != nil {
		return TaskChanges{}, err
	}
	files := parseNumstat(output)
	untracked, err := manager.gitOutput(ctx, item.Path, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return TaskChanges{}, err
	}
	for _, path := range strings.Split(untracked, "\n") {
		if path == "" {
			continue
		}
		additions, binary, countErr := countUntrackedLines(filepath.Join(item.Path, path))
		if countErr != nil {
			return TaskChanges{}, countErr
		}
		files = append(files, ChangedFile{Path: path, Status: "ADDED", Additions: additions, Binary: binary})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	result := TaskChanges{BaseCommitSHA: item.BaseCommitSHA, HeadSHA: item.HeadSHA, Dirty: item.Dirty, Files: files, FileCount: len(files)}
	for _, file := range files {
		result.Additions += file.Additions
		result.Deletions += file.Deletions
	}
	return result, nil
}

// TaskFileDiff 仅为变更清单中的路径按需返回单文件 Diff。
func (manager *Manager) TaskFileDiff(ctx context.Context, taskID, path string) (FileDiff, error) {
	changes, err := manager.TaskChanges(ctx, taskID)
	if err != nil {
		return FileDiff{}, err
	}
	var selected *ChangedFile
	for index := range changes.Files {
		if changes.Files[index].Path == path {
			selected = &changes.Files[index]
			break
		}
	}
	if selected == nil {
		return FileDiff{}, ErrChangeNotFound
	}
	item, err := manager.TaskWorkspace(ctx, taskID)
	if err != nil || item == nil {
		return FileDiff{}, err
	}
	var output []byte
	if selected.Status == "ADDED" {
		output, err = gitDiffAllowChanges(ctx, item.Path, "diff", "--no-index", "--", "/dev/null", path)
	} else {
		output, err = gitDiffAllowChanges(ctx, item.Path, "diff", "--no-ext-diff", "--unified=3", item.BaseCommitSHA, "--", path)
	}
	if err != nil {
		return FileDiff{}, err
	}
	truncated := len(output) > maxPatchBytes
	if truncated {
		output = output[:maxPatchBytes]
	}
	return FileDiff{Path: path, Patch: string(output), Truncated: truncated, Binary: selected.Binary}, nil
}

func parseNumstat(output string) []ChangedFile {
	files := make([]ChangedFile, 0)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		file := ChangedFile{Path: parts[2], Status: "MODIFIED"}
		if parts[0] == "-" || parts[1] == "-" {
			file.Binary = true
		} else {
			file.Additions, _ = strconv.Atoi(parts[0])
			file.Deletions, _ = strconv.Atoi(parts[1])
		}
		files = append(files, file)
	}
	return files
}

func countUntrackedLines(path string) (int, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, false, fmt.Errorf("read untracked file: %w", err)
	}
	if strings.IndexByte(string(content), 0) >= 0 {
		return 0, true, nil
	}
	count := strings.Count(string(content), "\n")
	if len(content) > 0 && content[len(content)-1] != '\n' {
		count++
	}
	return count, false, nil
}

func gitDiffAllowChanges(ctx context.Context, directory string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if err == nil || errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return output, nil
	}
	return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
}
