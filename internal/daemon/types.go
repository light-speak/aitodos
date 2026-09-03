package daemon

import "time"

// Metadata 描述当前项目前台进程的发现信息。
type Metadata struct {
	PID               int       `json:"pid"`
	Port              int       `json:"port"`
	URL               string    `json:"url"`
	ProjectInstanceID string    `json:"project_instance_id"`
	Nonce             string    `json:"nonce"`
	StartedAt         time.Time `json:"started_at"`
}

// Health 是项目服务的健康检查响应。
type Health struct {
	Status            string `json:"status"`
	ProjectInstanceID string `json:"project_instance_id"`
	Nonce             string `json:"nonce"`
	PID               int    `json:"pid"`
	Version           string `json:"version"`
	Commit            string `json:"commit"`
}
