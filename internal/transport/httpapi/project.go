package httpapi

import (
	"net/http"

	"github.com/light-speak/aitodos/internal/project"
)

type projectHandler struct {
	project         *project.Project
	schedulerStatus func() SchedulerStatus
}

// SchedulerStatus 是允许项目接口公开的最小调度健康信息。
type SchedulerStatus struct {
	ConsecutiveFailures int
	LastError           string
}

type updateWorkerSettingsRequest struct {
	Enabled    bool `json:"enabled"`
	MaxWorkers int  `json:"max_workers"`
}

type projectResponse struct {
	Name              string `json:"name"`
	Root              string `json:"root"`
	Agent             string `json:"agent"`
	WorkersEnabled    bool   `json:"workers_enabled"`
	MaxWorkers        int    `json:"max_workers"`
	SchedulerFailures int    `json:"scheduler_failures"`
	SchedulerError    string `json:"scheduler_error"`
}

// RegisterProjectRoutes 注册当前项目及本机 Worker 配置端点。
func RegisterProjectRoutes(mux *http.ServeMux, currentProject *project.Project, statusProviders ...func() SchedulerStatus) {
	statusProvider := func() SchedulerStatus { return SchedulerStatus{} }
	if len(statusProviders) > 0 && statusProviders[0] != nil {
		statusProvider = statusProviders[0]
	}
	handler := &projectHandler{project: currentProject, schedulerStatus: statusProvider}
	mux.HandleFunc("GET /api/project", handler.get)
	mux.HandleFunc("POST /api/project/workers", handler.updateWorkers)
}

func (handler *projectHandler) get(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, handler.response())
}

func (handler *projectHandler) updateWorkers(response http.ResponseWriter, request *http.Request) {
	var input updateWorkerSettingsRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Worker 配置")
		return
	}
	if _, err := handler.project.UpdateWorkerSettings(input.Enabled, input.MaxWorkers); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_WORKER_SETTINGS", err.Error())
		return
	}
	writeJSON(response, http.StatusOK, handler.response())
}

func (handler *projectHandler) response() projectResponse {
	settings := handler.project.WorkerSettings()
	status := handler.schedulerStatus()
	return projectResponse{
		Name:              handler.project.Config.Name,
		Root:              handler.project.Root,
		Agent:             handler.project.AgentAdapter(),
		WorkersEnabled:    settings.Enabled,
		MaxWorkers:        settings.MaxWorkers,
		SchedulerFailures: status.ConsecutiveFailures,
		SchedulerError:    status.LastError,
	}
}
