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
	if branch := remoteDefaultBranch(ctx, root); branch != "" {
		return branch
	}
	for _, branch := range []string{"main", "master", "trunk", "develop"} {
		if _, err := gitOutput(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return branch
		}
	}
	branch, _ := gitOutput(ctx, root, "symbolic-ref", "--short", "HEAD")
	return branch
}

func remoteDefaultBranch(ctx context.Context, root string) string {
	remotes, err := gitOutput(ctx, root, "remote")
	if err != nil {
		return ""
	}
	names := strings.Fields(remotes)
	for index, name := range names {
		if name == "origin" && index > 0 {
			names[0], names[index] = names[index], names[0]
			break
		}
	}
	for _, name := range names {
		ref, refErr := gitOutput(ctx, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+name+"/HEAD")
		if refErr == nil {
			return strings.TrimPrefix(ref, name+"/")
		}
	}
	return ""
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
