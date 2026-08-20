package project

import "sync"

// Paths 集中描述当前项目的受管路径。
type Paths struct {
	ATSRoot       string
	ProjectConfig string
	LocalConfig   string
	GitIgnore     string
	Database      string
	Artifacts     string
	Runtime       string
	Worktrees     string
	DaemonLock    string
	DaemonState   string
}

// ProjectConfig 是可以提交到 Git 的项目配置。
type ProjectConfig struct {
	Version       int    `toml:"version"`
	Name          string `toml:"name"`
	DefaultBranch string `toml:"default_branch"`
}

// LocalConfig 是不提交到 Git 的本机配置。
type LocalConfig struct {
	Version int          `toml:"version"`
	Agent   AgentConfig  `toml:"agent"`
	Proxy   ProxyConfig  `toml:"proxy"`
	Server  ServerConfig `toml:"server"`
}

// ServerConfig 描述当前项目本机 Web 服务的监听配置。
type ServerConfig struct {
	Port int `toml:"port"`
}

// AgentConfig 描述当前项目启动 Agent CLI 的方式。
type AgentConfig struct {
	Adapter        string   `toml:"adapter"`
	Command        string   `toml:"command"`
	Args           []string `toml:"args"`
	Model          string   `toml:"model"`
	WorkersEnabled bool     `toml:"workers_enabled"`
	MaxWorkers     int      `toml:"max_workers"`
}

// ProxyConfig 描述当前项目的网络代理继承策略。
type ProxyConfig struct {
	Mode string `toml:"mode"`
}

// Project 是一个已经初始化的本地项目。
type Project struct {
	configMu     sync.RWMutex
	Root         string
	GitCommonDir string
	InstanceID   string
	Paths        Paths
	Config       ProjectConfig
	Local        LocalConfig
}

// WorkerSettings 是当前项目可动态修改的 Worker 配置。
type WorkerSettings struct {
	Enabled    bool `json:"enabled"`
	MaxWorkers int  `json:"max_workers"`
}
