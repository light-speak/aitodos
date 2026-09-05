// Package mcpserver 提供当前项目有界、只读并可审计的 MCP stdio 服务。
package mcpserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/light-speak/aitodos/internal/buildinfo"
	"github.com/light-speak/aitodos/internal/domain/mcpaudit"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/storage"
)

const (
	currentProtocolVersion = "2025-11-25"
	maxMessageBytes        = 1 << 20
	maxResultBytes         = 256 << 10
)

var supportedProtocolVersions = map[string]struct{}{
	"2024-11-05":           {},
	"2025-03-26":           {},
	"2025-06-18":           {},
	currentProtocolVersion: {},
}

// Server 只提供当前项目的受限读取工具。
type Server struct {
	database    *sql.DB
	audit       *storage.MCPAuditStore
	clientName  string
	initialized bool
}

// New 创建项目 MCP Server。
func New(database *sql.DB) *Server {
	return &Server{database: database, audit: storage.NewMCPAuditStore(database), clientName: "unknown"}
}

// Serve 处理按行分隔的 JSON-RPC 消息，直到输入关闭或上下文取消。
func (server *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		response := server.handle(ctx, line)
		if response != nil {
			if err := encoder.Encode(response); err != nil {
				return fmt.Errorf("write MCP response: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		response := rpcErrorResponse(nil, -32700, "MCP 消息超过 1 MiB 或无法读取")
		if encodeErr := encoder.Encode(response); encodeErr != nil {
			return fmt.Errorf("write MCP read error: %w", encodeErr)
		}
	}
	return nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (server *Server) handle(ctx context.Context, content []byte) *rpcResponse {
	var request rpcRequest
	if err := json.Unmarshal(content, &request); err != nil || request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		return rpcErrorResponse(nil, -32700, "无效的 JSON-RPC 消息")
	}
	if len(request.ID) == 0 {
		server.handleNotification(request.Method)
		return nil
	}
	if request.Method == "initialize" {
		return server.initialize(request)
	}
	if !server.initialized {
		return rpcErrorResponse(request.ID, -32002, "MCP Server 尚未初始化")
	}
	switch request.Method {
	case "ping":
		return rpcResultResponse(request.ID, map[string]any{})
	case "tools/list":
		return rpcResultResponse(request.ID, map[string]any{"tools": toolDefinitions()})
	case "tools/call":
		return server.callTool(ctx, request)
	default:
		return rpcErrorResponse(request.ID, -32601, "不支持的方法")
	}
}

func (server *Server) handleNotification(method string) {
	if method == "notifications/initialized" {
		server.initialized = true
	}
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name string `json:"name"`
	} `json:"clientInfo"`
}

func (server *Server) initialize(request rpcRequest) *rpcResponse {
	var params initializeParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return rpcErrorResponse(request.ID, -32602, "initialize 参数无效")
	}
	protocol := params.ProtocolVersion
	if _, supported := supportedProtocolVersions[protocol]; !supported {
		protocol = currentProtocolVersion
	}
	server.clientName = strings.TrimSpace(params.ClientInfo.Name)
	if server.clientName == "" {
		server.clientName = "unknown"
	}
	server.initialized = true
	return rpcResultResponse(request.ID, map[string]any{
		"protocolVersion": protocol,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]string{"name": "aitodos-project", "version": buildinfo.Version},
		"instructions":    "只读访问当前 AiTodos 项目的规范数据；结果有界，原始日志和完整 Diff 不在此服务中返回。",
	})
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (server *Server) callTool(ctx context.Context, request rpcRequest) *rpcResponse {
	var params callToolParams
	if err := json.Unmarshal(request.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
		return rpcErrorResponse(request.ID, -32602, "tools/call 参数无效")
	}
	if len(params.Arguments) == 0 {
		params.Arguments = json.RawMessage(`{}`)
	}
	callID, err := randomCallID()
	if err != nil {
		return rpcErrorResponse(request.ID, -32603, "无法创建 MCP 审计标识")
	}
	keys, digest, err := summarizeArguments(params.Arguments)
	if err != nil {
		return rpcErrorResponse(request.ID, -32602, "工具参数必须是 JSON 对象")
	}
	started := storage.MCPAuditInput{
		CallID: callID, ClientName: server.clientName, ToolName: params.Name,
		Phase: mcpaudit.PhaseStarted, ArgumentKeys: keys, ArgumentsSHA256: digest,
	}
	if _, err := server.audit.Append(ctx, started); err != nil {
		return rpcErrorResponse(request.ID, -32603, "无法记录 MCP 调用审计")
	}
	result, callErr := server.executeTool(ctx, params.Name, params.Arguments)
	if callErr != nil {
		server.finishAudit(ctx, started, mcpaudit.PhaseFailed, nil, callErr)
		return rpcErrorResponse(request.ID, -32602, callErr.Error())
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		server.finishAudit(ctx, started, mcpaudit.PhaseFailed, nil, err)
		return rpcErrorResponse(request.ID, -32603, "无法编码工具结果")
	}
	if len(encoded) > maxResultBytes {
		err = errors.New("工具结果超过 256 KiB，请缩小查询范围")
		server.finishAudit(ctx, started, mcpaudit.PhaseFailed, nil, err)
		return rpcErrorResponse(request.ID, -32602, err.Error())
	}
	resultBytes := int64(len(encoded))
	if err := server.finishAudit(ctx, started, mcpaudit.PhaseCompleted, &resultBytes, nil); err != nil {
		return rpcErrorResponse(request.ID, -32603, "无法完成 MCP 调用审计")
	}
	return rpcResultResponse(request.ID, map[string]any{
		"content":           []map[string]string{{"type": "text", "text": string(encoded)}},
		"structuredContent": result,
	})
}

func (server *Server) finishAudit(ctx context.Context, started storage.MCPAuditInput, phase mcpaudit.Phase, resultBytes *int64, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, err := server.audit.Append(ctx, storage.MCPAuditInput{
		CallID: started.CallID, ClientName: started.ClientName, ToolName: started.ToolName,
		Phase: phase, ArgumentKeys: started.ArgumentKeys, ArgumentsSHA256: started.ArgumentsSHA256,
		ResultBytes: resultBytes, ErrorMessage: message,
	})
	return err
}

func summarizeArguments(arguments json.RawMessage) ([]string, string, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &values); err != nil || values == nil {
		return nil, "", errors.New("arguments must be an object")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.Sum256(arguments)
	return keys, hex.EncodeToString(digest[:]), nil
}

func randomCallID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "mcp-" + hex.EncodeToString(value), nil
}

func rpcResultResponse(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcErrorResponse(id json.RawMessage, code int, message string) *rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func (server *Server) executeTool(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
	switch name {
	case "search_items":
		return server.searchItems(ctx, arguments)
	case "get_topic":
		return server.getTopic(ctx, arguments)
	case "get_task":
		return server.getTask(ctx, arguments)
	case "get_plan_for_topic":
		return server.getPlan(ctx, arguments)
	case "get_thread":
		return server.getThread(ctx, arguments)
	case "get_task_relations":
		return server.getTaskRelations(ctx, arguments)
	case "get_task_runs":
		return server.getTaskRuns(ctx, arguments)
	case "get_decisions":
		return server.getDecisions(ctx, arguments)
	case "get_labels":
		return server.getLabels(ctx, arguments)
	case "get_run_summary":
		return server.getRunSummary(ctx, arguments)
	case "get_ci_status":
		return server.getCIStatus(ctx, arguments)
	case "get_clarifications":
		return server.getClarifications(ctx, arguments)
	case "get_experience":
		return server.getExperience(ctx, arguments)
	case "get_recalled_experiences":
		return server.getRecalledExperiences(ctx, arguments)
	case "get_current_objective":
		return server.getCurrentObjective(ctx, arguments)
	case "get_objective_checkpoints":
		return server.getObjectiveCheckpoints(ctx, arguments)
	default:
		return nil, fmt.Errorf("未知或不可写的工具 %q", name)
	}
}

func (server *Server) getDecisions(ctx context.Context, arguments json.RawMessage) (any, error) {
	subjectKind, subjectID, err := requiredSubject(arguments)
	if err != nil {
		return nil, err
	}
	var options struct {
		IncludeSuperseded bool `json:"include_superseded"`
	}
	if err := json.Unmarshal(arguments, &options); err != nil {
		return nil, errors.New("get_decisions 参数无效")
	}
	store := storage.NewKnowledgeStore(server.database)
	if subjectKind == "TOPIC" {
		return store.ListTopicDecisions(ctx, subjectID, options.IncludeSuperseded)
	}
	return store.ListTaskDecisions(ctx, subjectID, options.IncludeSuperseded)
}

func (server *Server) getLabels(ctx context.Context, arguments json.RawMessage) (any, error) {
	subjectKind, subjectID, err := requiredSubject(arguments)
	if err != nil {
		return nil, err
	}
	store := storage.NewKnowledgeStore(server.database)
	if subjectKind == "TOPIC" {
		return store.ListTopicLabels(ctx, subjectID)
	}
	return store.ListTaskLabels(ctx, subjectID)
}

func (server *Server) getRunSummary(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "run_id")
	if err != nil {
		return nil, err
	}
	return storage.NewKnowledgeStore(server.database).GetRunSummary(ctx, id)
}

func (server *Server) getCIStatus(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "task_id")
	if err != nil {
		return nil, err
	}
	return storage.NewKnowledgeStore(server.database).ListCISnapshots(ctx, id, 20)
}

func (server *Server) getClarifications(ctx context.Context, arguments json.RawMessage) (any, error) {
	subjectKind, subjectID, err := requiredSubject(arguments)
	if err != nil {
		return nil, err
	}
	store := storage.NewClarificationStore(server.database)
	if subjectKind == "TOPIC" {
		return store.ListTopic(ctx, subjectID)
	}
	return store.ListTask(ctx, subjectID)
}

func (server *Server) getExperience(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "experience_id")
	if err != nil {
		return nil, err
	}
	return storage.NewExperienceStore(server.database).Get(ctx, id)
}

func (server *Server) getRecalledExperiences(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "run_id")
	if err != nil {
		return nil, err
	}
	return storage.NewExperienceStore(server.database).ListRunRecalls(ctx, id)
}

func (server *Server) getCurrentObjective(ctx context.Context, _ json.RawMessage) (any, error) {
	view, err := storage.NewObjectiveStore(server.database).GetCurrent(ctx)
	if errors.Is(err, storage.ErrObjectiveNotFound) {
		return nil, nil
	}
	return view, err
}

func (server *Server) getObjectiveCheckpoints(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "objective_id")
	if err != nil {
		return nil, err
	}
	return storage.NewObjectiveStore(server.database).ListCheckpoints(ctx, id)
}

func requiredSubject(arguments json.RawMessage) (string, string, error) {
	var input struct {
		SubjectKind string `json:"subject_kind"`
		SubjectID   string `json:"subject_id"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil || strings.TrimSpace(input.SubjectID) == "" {
		return "", "", errors.New("需要 subject_kind 和 subject_id")
	}
	kind := strings.ToUpper(strings.TrimSpace(input.SubjectKind))
	if kind != "TOPIC" && kind != "TASK" {
		return "", "", errors.New("subject_kind 必须是 TOPIC 或 TASK")
	}
	return kind, strings.TrimSpace(input.SubjectID), nil
}

func (server *Server) searchItems(ctx context.Context, arguments json.RawMessage) (any, error) {
	var input struct {
		Text        string        `json:"text"`
		Kinds       []search.Kind `json:"kinds"`
		Statuses    []string      `json:"statuses"`
		OnlyCurrent bool          `json:"only_current"`
		Limit       int           `json:"limit"`
		Cursor      string        `json:"cursor"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return nil, errors.New("search_items 参数无效")
	}
	return storage.NewSearchStore(server.database).Search(ctx, search.Query{
		Text: input.Text, Kinds: input.Kinds, Statuses: input.Statuses,
		OnlyCurrent: input.OnlyCurrent, Limit: input.Limit, Cursor: input.Cursor,
	})
}

func (server *Server) getTopic(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "topic_id")
	if err != nil {
		return nil, err
	}
	return storage.NewTopicStore(server.database).Get(ctx, id)
}

func (server *Server) getTask(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "task_id")
	if err != nil {
		return nil, err
	}
	return storage.NewTaskStore(server.database).Get(ctx, id)
}

func (server *Server) getPlan(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "topic_id")
	if err != nil {
		return nil, err
	}
	return storage.NewPlanStore(server.database).GetByTopic(ctx, id)
}

func (server *Server) getThread(ctx context.Context, arguments json.RawMessage) (any, error) {
	var input struct {
		SubjectKind string `json:"subject_kind"`
		SubjectID   string `json:"subject_id"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil || strings.TrimSpace(input.SubjectID) == "" {
		return nil, errors.New("get_thread 需要 subject_kind 和 subject_id")
	}
	store := storage.NewDiscussionStore(server.database)
	switch strings.ToUpper(strings.TrimSpace(input.SubjectKind)) {
	case "TOPIC":
		return store.ListTopicMessages(ctx, strings.TrimSpace(input.SubjectID))
	case "TASK":
		return store.ListTaskMessages(ctx, strings.TrimSpace(input.SubjectID))
	default:
		return nil, errors.New("subject_kind 必须是 TOPIC 或 TASK")
	}
}

func (server *Server) getTaskRelations(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "task_id")
	if err != nil {
		return nil, err
	}
	store := storage.NewRelationStore(server.database)
	tasks, err := store.ListTaskRelations(ctx, id)
	if err != nil {
		return nil, err
	}
	topics, err := store.ListTaskTopics(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": tasks, "topics": topics}, nil
}

func (server *Server) getTaskRuns(ctx context.Context, arguments json.RawMessage) (any, error) {
	id, err := requiredString(arguments, "task_id")
	if err != nil {
		return nil, err
	}
	return storage.NewRunStore(server.database).Query(ctx, storage.RunQuery{
		TaskID: id, Limit: 50, Purpose: domainrun.Purpose(""),
	})
}

func requiredString(arguments json.RawMessage, key string) (string, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &values); err != nil {
		return "", errors.New("工具参数无效")
	}
	var value string
	if err := json.Unmarshal(values[key], &value); err != nil || strings.TrimSpace(value) == "" || len(value) > 500 {
		return "", fmt.Errorf("%s 必须是非空字符串", key)
	}
	return strings.TrimSpace(value), nil
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func toolDefinitions() []toolDefinition {
	identifier := func(name string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false,
			"properties": map[string]any{name: map[string]string{"type": "string"}}, "required": []string{name}}
	}
	subject := func(extra map[string]any) map[string]any {
		properties := map[string]any{
			"subject_kind": map[string]any{"type": "string", "enum": []string{"TOPIC", "TASK"}},
			"subject_id":   map[string]string{"type": "string"},
		}
		for key, value := range extra {
			properties[key] = value
		}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties,
			"required": []string{"subject_kind", "subject_id"}}
	}
	return []toolDefinition{
		{Name: "search_items", Description: "全文搜索当前项目的长期目标、检查点、规范条目、讨论、决策、经验、摘要、标签和 CI 检查。",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"text": map[string]string{"type": "string"}, "kinds": map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
				"statuses": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "only_current": map[string]string{"type": "boolean"},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}, "cursor": map[string]string{"type": "string"}}, "required": []string{"text"}}},
		{Name: "get_topic", Description: "按 ID 读取 Topic。", InputSchema: identifier("topic_id")},
		{Name: "get_task", Description: "按 ID 读取 Task。", InputSchema: identifier("task_id")},
		{Name: "get_plan_for_topic", Description: "读取 Topic 当前 Plan 及不可变 Revision。", InputSchema: identifier("topic_id")},
		{Name: "get_thread", Description: "读取 Topic 或 Task 的讨论线程。", InputSchema: map[string]any{"type": "object", "additionalProperties": false,
			"properties": map[string]any{"subject_kind": map[string]any{"type": "string", "enum": []string{"TOPIC", "TASK"}}, "subject_id": map[string]string{"type": "string"}},
			"required":   []string{"subject_kind", "subject_id"}}},
		{Name: "get_task_relations", Description: "读取 Task 的 Task 与 Topic 关联。", InputSchema: identifier("task_id")},
		{Name: "get_task_runs", Description: "读取 Task 最近 50 次 Run 的状态和摘要字段。", InputSchema: identifier("task_id")},
		{Name: "get_decisions", Description: "读取 Topic 或 Task 的有效决策。", InputSchema: subject(map[string]any{
			"include_superseded": map[string]string{"type": "boolean"},
		})},
		{Name: "get_labels", Description: "读取 Topic 或 Task 的展示标签。", InputSchema: subject(nil)},
		{Name: "get_run_summary", Description: "读取一个 Run 的可重建摘要和测试计数。", InputSchema: identifier("run_id")},
		{Name: "get_ci_status", Description: "读取 Task 最近 20 个显式导入的 CI 状态快照。", InputSchema: identifier("task_id")},
		{Name: "get_clarifications", Description: "读取 Topic 或 Task 的结构化问题和人工答案。", InputSchema: subject(nil)},
		{Name: "get_experience", Description: "按 ID 读取一条经验的完整指导、适用条件、证据状态和统计。", InputSchema: identifier("experience_id")},
		{Name: "get_recalled_experiences", Description: "读取一个 Run 实际召回的经验及当时的可解释评分。", InputSchema: identifier("run_id")},
		{Name: "get_current_objective", Description: "读取当前活跃或暂停的长期目标、完成条件、最近检查点和派生进度。",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false}},
		{Name: "get_objective_checkpoints", Description: "读取长期目标的不可变检查点历史。", InputSchema: identifier("objective_id")},
	}
}
