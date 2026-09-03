package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/light-speak/aitodos/internal/storage"
	"github.com/pelletier/go-toml/v2"
)

const gitIgnoreContent = `state.db*
local.toml
artifacts/
backups/
runtime/
worktrees/
`

// Initialize 在 Git 根目录创建或修复项目级 ATS 状态。
func Initialize(ctx context.Context, start string) (*Project, bool, error) {
	root, commonDir, err := discoverRepository(ctx, start)
	if err != nil {
		return nil, false, err
	}
	paths := buildPaths(root)
	created := !pathExists(paths.ProjectConfig)

	if err := createDirectories(paths); err != nil {
		return nil, false, err
	}
	if err := initializeConfigFiles(ctx, root, paths); err != nil {
		return nil, false, err
	}

	config, local, err := readConfigs(paths)
	if err != nil {
		return nil, false, err
	}
	instanceID, err := existingOrNewInstanceID(ctx, paths.Database)
	if err != nil {
		return nil, false, err
	}
	database, err := storage.Open(ctx, paths.Database, storage.ProjectMetadata{
		InstanceID:   instanceID,
		Name:         config.Name,
		RepoRoot:     root,
		GitCommonDir: commonDir,
	})
	if err != nil {
		return nil, false, err
	}
	defer database.Close()
	metadata, err := storage.ReadProjectMetadata(ctx, database)
	if err != nil {
		return nil, false, err
	}

	return &Project{
		Root:         root,
		GitCommonDir: commonDir,
		InstanceID:   metadata.InstanceID,
		Paths:        paths,
		Config:       config,
		Local:        local,
	}, created, nil
}

// Load 加载已执行过 ats init 的项目。
func Load(ctx context.Context, start string) (*Project, error) {
	root, commonDir, err := discoverRepository(ctx, start)
	if err != nil {
		return nil, err
	}
	paths := buildPaths(root)
	if !pathExists(paths.ProjectConfig) || !pathExists(paths.Database) {
		return nil, errors.New("当前项目尚未初始化，请先运行 ats init")
	}
	config, local, err := readConfigs(paths)
	if err != nil {
		return nil, err
	}
	database, err := storage.OpenExisting(ctx, paths.Database)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	metadata, err := storage.ReadProjectMetadata(ctx, database)
	if err != nil {
		return nil, err
	}
	if metadata.GitCommonDir != commonDir {
		return nil, fmt.Errorf("项目 Git 身份不匹配: 数据库=%q 当前=%q", metadata.GitCommonDir, commonDir)
	}
	return &Project{
		Root:         root,
		GitCommonDir: commonDir,
		InstanceID:   metadata.InstanceID,
		Paths:        paths,
		Config:       config,
		Local:        local,
	}, nil
}

func buildPaths(root string) Paths {
	atsRoot := filepath.Join(root, ".ats")
	runtime := filepath.Join(atsRoot, "runtime")
	return Paths{
		ATSRoot:       atsRoot,
		ProjectConfig: filepath.Join(atsRoot, "project.toml"),
		LocalConfig:   filepath.Join(atsRoot, "local.toml"),
		GitIgnore:     filepath.Join(atsRoot, ".gitignore"),
		Database:      filepath.Join(atsRoot, "state.db"),
		Artifacts:     filepath.Join(atsRoot, "artifacts"),
		Runtime:       runtime,
		Worktrees:     filepath.Join(atsRoot, "worktrees"),
		DaemonLock:    filepath.Join(runtime, "daemon.lock"),
		DaemonState:   filepath.Join(runtime, "daemon.json"),
	}
}

func createDirectories(paths Paths) error {
	directories := []string{paths.ATSRoot, paths.Artifacts, paths.Runtime, paths.Worktrees}
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("创建目录 %q: %w", directory, err)
		}
	}
	return nil
}

func initializeConfigFiles(ctx context.Context, root string, paths Paths) error {
	if !pathExists(paths.ProjectConfig) {
		config := ProjectConfig{
			Version:       1,
			Name:          filepath.Base(root),
			DefaultBranch: defaultBranch(ctx, root),
		}
		if err := writeTOML(paths.ProjectConfig, 0o644, config); err != nil {
			return err
		}
	}
	if !pathExists(paths.LocalConfig) {
		local := LocalConfig{
			Version: 1,
			Agent: AgentConfig{
				Args:           []string{},
				WorkersEnabled: false,
				MaxWorkers:     2,
			},
			Proxy:  ProxyConfig{Mode: "inherit"},
			Server: ServerConfig{Port: 0},
		}
		if err := writeTOML(paths.LocalConfig, 0o600, local); err != nil {
			return err
		}
	}
	if !pathExists(paths.GitIgnore) {
		if err := atomicWrite(paths.GitIgnore, 0o644, []byte(gitIgnoreContent)); err != nil {
			return err
		}
	}
	return nil
}

func readConfigs(paths Paths) (ProjectConfig, LocalConfig, error) {
	var config ProjectConfig
	if err := readTOML(paths.ProjectConfig, &config); err != nil {
		return ProjectConfig{}, LocalConfig{}, err
	}
	var local LocalConfig
	if err := readTOML(paths.LocalConfig, &local); err != nil {
		return ProjectConfig{}, LocalConfig{}, err
	}
	if config.Version != 1 || local.Version != 1 {
		return ProjectConfig{}, LocalConfig{}, errors.New("不支持的 ATS 配置版本")
	}
	if err := validateMaxWorkers(local.Agent.MaxWorkers); err != nil {
		return ProjectConfig{}, LocalConfig{}, err
	}
	if local.Server.Port < 0 || local.Server.Port > 65535 {
		return ProjectConfig{}, LocalConfig{}, errors.New("server.port 必须为 0 到 65535；0 表示使用随机端口")
	}
	return config, local, nil
}

// WorkerSettings 返回当前进程生效的项目 Worker 配置。
func (project *Project) WorkerSettings() WorkerSettings {
	project.configMu.RLock()
	defer project.configMu.RUnlock()
	return WorkerSettings{
		Enabled:    project.Local.Agent.WorkersEnabled,
		MaxWorkers: project.Local.Agent.MaxWorkers,
	}
}

// AgentAdapter 返回旧配置中的 Adapter 展示值。
func (project *Project) AgentAdapter() string {
	project.configMu.RLock()
	defer project.configMu.RUnlock()
	return project.Local.Agent.Adapter
}

// UpdateWorkerSettings 原子保存并启用新的项目 Worker 配置。
func (project *Project) UpdateWorkerSettings(enabled bool, maxWorkers int) (WorkerSettings, error) {
	if err := validateMaxWorkers(maxWorkers); err != nil {
		return WorkerSettings{}, err
	}

	project.configMu.Lock()
	defer project.configMu.Unlock()
	updated := project.Local
	updated.Agent.WorkersEnabled = enabled
	updated.Agent.MaxWorkers = maxWorkers
	if err := writeTOML(project.Paths.LocalConfig, 0o600, updated); err != nil {
		return WorkerSettings{}, fmt.Errorf("保存 Worker 配置: %w", err)
	}
	project.Local = updated
	return WorkerSettings{Enabled: enabled, MaxWorkers: maxWorkers}, nil
}

func validateMaxWorkers(maxWorkers int) error {
	if maxWorkers < 1 || maxWorkers > 32 {
		return errors.New("agent.max_workers 必须为 1 到 32")
	}
	return nil
}

func existingOrNewInstanceID(ctx context.Context, path string) (string, error) {
	if pathExists(path) {
		database, err := storage.OpenExisting(ctx, path)
		if err == nil {
			defer database.Close()
			metadata, readErr := storage.ReadProjectMetadata(ctx, database)
			if readErr == nil {
				return metadata.InstanceID, nil
			}
		}
	}
	return randomID()
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成项目实例 ID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]),
	), nil
}

func writeTOML(path string, mode os.FileMode, value any) error {
	content, err := toml.Marshal(value)
	if err != nil {
		return fmt.Errorf("编码 TOML %q: %w", path, err)
	}
	return atomicWrite(path, mode, content)
}

func readTOML(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置 %q: %w", path, err)
	}
	if err := toml.Unmarshal(content, target); err != nil {
		return fmt.Errorf("解析配置 %q: %w", path, err)
	}
	return nil
}

func atomicWrite(path string, mode os.FileMode, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".ats-write-*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("替换文件 %q: %w", path, err)
	}
	return nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
