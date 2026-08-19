package project

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func discoverRepository(ctx context.Context, start string) (string, string, error) {
	root, err := gitOutput(ctx, start, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", fmt.Errorf("当前目录不在 Git 仓库中: %w", err)
	}
	root, err = canonicalPath(root)
	if err != nil {
		return "", "", fmt.Errorf("解析 Git 根目录: %w", err)
	}

	commonDir, err := gitOutput(ctx, root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", "", fmt.Errorf("读取 git common dir: %w", err)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(root, commonDir)
	}
	commonDir, err = canonicalPath(commonDir)
	if err != nil {
		return "", "", fmt.Errorf("解析 git common dir: %w", err)
	}
	return root, commonDir, nil
}

func defaultBranch(ctx context.Context, root string) string {
	branch, err := gitOutput(ctx, root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return branch
}

func gitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.CommandContext(ctx, "git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
