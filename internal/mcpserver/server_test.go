package mcpserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/experience"
	"github.com/light-speak/aitodos/internal/domain/knowledge"
	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestServerInitializesListsAndCallsReadOnlyTools(t *testing.T) {
	database := openMCPTestDatabase(t)
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Title: "实现全文搜索", Description: "让 Agent 查找历史设定", AcceptanceCriteria: "返回有界结果", Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"codex","version":"1"},"capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_task","arguments":{"task_id":"` + created.ID + `"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	server := New(database)
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 3 {
		t.Fatalf("response count = %d, output = %s", len(responses), output.String())
	}
	if responses[0].Error != nil || responses[0].Result == nil || responses[1].Error != nil || responses[2].Error != nil {
		t.Fatalf("responses = %#v", responses)
	}
	if !bytes.Contains(responses[2].Result, []byte(created.Key)) {
		t.Fatalf("task response = %s", responses[2].Result)
	}
	items, err := storage.NewMCPAuditStore(database).List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ToolName != "get_task" || items[1].Phase != "COMPLETED" {
		t.Fatalf("audit = %#v", items)
	}
}

func TestServerReadsProjectKnowledge(t *testing.T) {
	database := openMCPTestDatabase(t)
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{Title: "发布版本"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := storage.NewKnowledgeStore(database).CreateDecision(context.Background(), knowledge.DecisionInput{
		TaskID: created.ID, Title: "只通过 PR 合并", Content: "禁止自动 push",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"test"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_decisions","arguments":{"subject_kind":"TASK","subject_id":"` + created.ID + `"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := New(database).Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 2 || responses[1].Error != nil || !bytes.Contains(responses[1].Result, []byte(decision.ID)) {
		t.Fatalf("responses = %#v, output = %s", responses, output.String())
	}
}

func TestServerReadsExperienceDetails(t *testing.T) {
	database := openMCPTestDatabase(t)
	createdTask, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "复用经验"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewExperienceStore(database).CreateVerified(t.Context(), experience.Input{
		TaskID: createdTask.ID, Title: "先验证", Summary: "先运行最小测试",
		Guidance: "保留命令退出码", Applicability: "修改核心状态机",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := New(database)
	result, err := server.getExperience(t.Context(), encodeArguments(t, map[string]any{"experience_id": created.ID}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if !bytes.Contains(encoded, []byte("保留命令退出码")) {
		t.Fatalf("experience = %s", encoded)
	}
}

func TestServerRejectsCallsBeforeInitializeAndUnknownTools(t *testing.T) {
	database := openMCPTestDatabase(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_task","arguments":{"secret":"do-not-store"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := New(database).Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 3 || responses[0].Error == nil || responses[1].Error != nil || responses[2].Error == nil {
		t.Fatalf("responses = %#v", responses)
	}
	items, err := storage.NewMCPAuditStore(database).List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(items)
	if bytes.Contains(encoded, []byte("do-not-store")) {
		t.Fatalf("audit leaked arguments: %s", encoded)
	}
}

func TestServerRejectsMalformedAndOversizedMessages(t *testing.T) {
	database := openMCPTestDatabase(t)
	var output bytes.Buffer
	input := "not-json\n" + strings.Repeat("x", maxMessageBytes+1) + "\n"
	if err := New(database).Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 2 || responses[0].Error == nil || responses[1].Error == nil {
		t.Fatalf("responses = %#v", responses)
	}
}

func TestServerHandlesProtocolEdgesAndWriteFailures(t *testing.T) {
	database := openMCPTestDatabase(t)
	server := New(database)
	requests := strings.Join([]string{
		" ",
		`{"jsonrpc":"2.0","method":"notifications/unknown"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":[]}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"future","clientInfo":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(requests), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 3 || responses[0].Error == nil || responses[1].Error != nil || responses[2].Error != nil {
		t.Fatalf("responses = %#v", responses)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(database).Serve(canceled, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"), io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Serve() error = %v", err)
	}
	if err := New(database).Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"), failingWriter{}); err == nil {
		t.Fatal("Serve() accepted a response writer failure")
	}
	oversized := strings.NewReader(strings.Repeat("x", maxMessageBytes+1) + "\n")
	if err := New(database).Serve(context.Background(), oversized, failingWriter{}); err == nil {
		t.Fatal("Serve() accepted a read-error response writer failure")
	}
}

func TestServerReadToolsReturnStorageErrorsWithoutLeakingData(t *testing.T) {
	database := openMCPTestDatabase(t)
	server := New(database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	calls := []func() error{
		func() error {
			_, err := server.searchItems(t.Context(), json.RawMessage(`{"text":"query"}`))
			return err
		},
		func() error {
			_, err := server.getTopic(t.Context(), json.RawMessage(`{"topic_id":"topic"}`))
			return err
		},
		func() error { _, err := server.getTask(t.Context(), json.RawMessage(`{"task_id":"task"}`)); return err },
		func() error {
			_, err := server.getPlan(t.Context(), json.RawMessage(`{"topic_id":"topic"}`))
			return err
		},
		func() error {
			_, err := server.getThread(t.Context(), json.RawMessage(`{"subject_kind":"TOPIC","subject_id":"topic"}`))
			return err
		},
		func() error {
			_, err := server.getThread(t.Context(), json.RawMessage(`{"subject_kind":"TASK","subject_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getTaskRelations(t.Context(), json.RawMessage(`{"task_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getTaskRuns(t.Context(), json.RawMessage(`{"task_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getDecisions(t.Context(), json.RawMessage(`{"subject_kind":"TOPIC","subject_id":"topic"}`))
			return err
		},
		func() error {
			_, err := server.getDecisions(t.Context(), json.RawMessage(`{"subject_kind":"TASK","subject_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getLabels(t.Context(), json.RawMessage(`{"subject_kind":"TOPIC","subject_id":"topic"}`))
			return err
		},
		func() error {
			_, err := server.getLabels(t.Context(), json.RawMessage(`{"subject_kind":"TASK","subject_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getRunSummary(t.Context(), json.RawMessage(`{"run_id":"run"}`))
			return err
		},
		func() error {
			_, err := server.getCIStatus(t.Context(), json.RawMessage(`{"task_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getClarifications(t.Context(), json.RawMessage(`{"subject_kind":"TOPIC","subject_id":"topic"}`))
			return err
		},
		func() error {
			_, err := server.getClarifications(t.Context(), json.RawMessage(`{"subject_kind":"TASK","subject_id":"task"}`))
			return err
		},
		func() error {
			_, err := server.getExperience(t.Context(), json.RawMessage(`{"experience_id":"experience"}`))
			return err
		},
		func() error {
			_, err := server.getRecalledExperiences(t.Context(), json.RawMessage(`{"run_id":"run"}`))
			return err
		},
	}
	for index, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("closed database call %d unexpectedly succeeded", index)
		}
	}
	server.initialized = true
	response := server.callTool(t.Context(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call",
		Params: json.RawMessage(`{"name":"get_task","arguments":{"task_id":"task"}}`),
	})
	if response.Error == nil || response.Error.Code != -32603 {
		t.Fatalf("audit failure response = %#v", response)
	}
}

func TestServerDispatchesEveryBoundedReadTool(t *testing.T) {
	database := openMCPTestDatabase(t)
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"future","clientInfo":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_items","arguments":{"text":"不存在","limit":1}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_topic","arguments":{"topic_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_plan_for_topic","arguments":{"topic_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_thread","arguments":{"subject_kind":"TOPIC","subject_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_task_relations","arguments":{"task_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_task_runs","arguments":{"task_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_labels","arguments":{"subject_kind":"TASK","subject_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_run_summary","arguments":{"run_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"get_ci_status","arguments":{"task_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"get_clarifications","arguments":{"subject_kind":"TASK","subject_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"get_experience","arguments":{"experience_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"get_recalled_experiences","arguments":{"run_id":"missing"}}}`,
		`{"jsonrpc":"2.0","id":14,"method":"no/such/method","params":{}}`,
	}
	var output bytes.Buffer
	if err := New(database).Serve(context.Background(), strings.NewReader(strings.Join(requests, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != len(requests) || responses[0].Error != nil || responses[1].Error != nil || responses[len(responses)-1].Error == nil {
		t.Fatalf("responses = %#v", responses)
	}
}

func TestServerReadsBothSubjectKindsAndRelations(t *testing.T) {
	ctx := context.Background()
	database := openMCPTestDatabase(t)
	topicItem, err := storage.NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "讨论发布策略"})
	if err != nil {
		t.Fatal(err)
	}
	taskStore := storage.NewTaskStore(database)
	firstTask, err := taskStore.Create(ctx, task.CreateInput{Title: "实现发布策略"})
	if err != nil {
		t.Fatal(err)
	}
	secondTask, err := taskStore.Create(ctx, task.CreateInput{Title: "验证发布策略"})
	if err != nil {
		t.Fatal(err)
	}
	discussionStore := storage.NewDiscussionStore(database)
	if _, err := discussionStore.AppendTopicMessage(ctx, topicItem.ID, discussion.CreateMessageInput{Content: "Topic 消息"}); err != nil {
		t.Fatal(err)
	}
	if _, err := discussionStore.AppendTaskMessage(ctx, firstTask.ID, discussion.CreateMessageInput{Content: "Task 消息"}); err != nil {
		t.Fatal(err)
	}
	relationStore := storage.NewRelationStore(database)
	if err := relationStore.LinkTopicTask(ctx, topicItem.ID, firstTask.ID); err != nil {
		t.Fatal(err)
	}
	if err := relationStore.LinkTasksTyped(ctx, firstTask.ID, secondTask.ID, relation.TypeBlocks); err != nil {
		t.Fatal(err)
	}
	knowledgeStore := storage.NewKnowledgeStore(database)
	label, err := knowledgeStore.CreateLabel(ctx, knowledge.LabelInput{Name: "release", Color: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	if err := knowledgeStore.AttachTopicLabel(ctx, topicItem.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	if err := knowledgeStore.AttachTaskLabel(ctx, firstTask.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledgeStore.CreateDecision(ctx, knowledge.DecisionInput{TopicID: topicItem.ID, Title: "Topic 决策", Content: "先讨论"}); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledgeStore.CreateDecision(ctx, knowledge.DecisionInput{TaskID: firstTask.ID, Title: "Task 决策", Content: "只本地合并"}); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledgeStore.CreateCISnapshot(ctx, firstTask.ID, knowledge.CISnapshotInput{
		Provider: "github", CommitSHA: "1234567", State: "PASSED",
		Checks: []knowledge.CICheck{{Name: "test", State: "PASSED"}},
	}); err != nil {
		t.Fatal(err)
	}

	server := New(database)
	tests := []struct {
		name      string
		arguments map[string]any
		call      func(context.Context, json.RawMessage) (any, error)
	}{
		{name: "topic thread", arguments: map[string]any{"subject_kind": "TOPIC", "subject_id": topicItem.ID}, call: server.getThread},
		{name: "task thread", arguments: map[string]any{"subject_kind": "TASK", "subject_id": firstTask.ID}, call: server.getThread},
		{name: "topic decisions", arguments: map[string]any{"subject_kind": "TOPIC", "subject_id": topicItem.ID}, call: server.getDecisions},
		{name: "task decisions", arguments: map[string]any{"subject_kind": "TASK", "subject_id": firstTask.ID}, call: server.getDecisions},
		{name: "topic labels", arguments: map[string]any{"subject_kind": "TOPIC", "subject_id": topicItem.ID}, call: server.getLabels},
		{name: "task labels", arguments: map[string]any{"subject_kind": "TASK", "subject_id": firstTask.ID}, call: server.getLabels},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, callErr := test.call(ctx, encodeArguments(t, test.arguments))
			if callErr != nil {
				t.Fatal(callErr)
			}
			encoded, marshalErr := json.Marshal(result)
			if marshalErr != nil || len(encoded) <= 2 {
				t.Fatalf("result = %#v, marshal error = %v", result, marshalErr)
			}
		})
	}
	relations, err := server.getTaskRelations(ctx, encodeArguments(t, map[string]any{"task_id": firstTask.ID}))
	if err != nil {
		t.Fatal(err)
	}
	encodedRelations, _ := json.Marshal(relations)
	if !bytes.Contains(encodedRelations, []byte(secondTask.ID)) || !bytes.Contains(encodedRelations, []byte(topicItem.ID)) {
		t.Fatalf("relations = %s", encodedRelations)
	}
	ci, err := server.getCIStatus(ctx, encodeArguments(t, map[string]any{"task_id": firstTask.ID}))
	if err != nil {
		t.Fatal(err)
	}
	encodedCI, _ := json.Marshal(ci)
	if !bytes.Contains(encodedCI, []byte("github")) {
		t.Fatalf("CI = %s", encodedCI)
	}
}

func TestServerRejectsInvalidToolArguments(t *testing.T) {
	server := New(openMCPTestDatabase(t))
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{name: "invalid required string JSON", call: func() error { _, err := requiredString(json.RawMessage(`[]`), "task_id"); return err }},
		{name: "missing required string", call: func() error { _, err := requiredString(json.RawMessage(`{}`), "task_id"); return err }},
		{name: "oversized required string", call: func() error {
			_, err := requiredString(encodeArguments(t, map[string]any{"task_id": strings.Repeat("x", 501)}), "task_id")
			return err
		}},
		{name: "invalid subject", call: func() error {
			_, _, err := requiredSubject(json.RawMessage(`{"subject_kind":"PROJECT","subject_id":"one"}`))
			return err
		}},
		{name: "missing subject", call: func() error { _, _, err := requiredSubject(json.RawMessage(`{}`)); return err }},
		{name: "invalid thread subject", call: func() error {
			_, err := server.getThread(ctx, json.RawMessage(`{"subject_kind":"PROJECT","subject_id":"one"}`))
			return err
		}},
		{name: "invalid search", call: func() error { _, err := server.searchItems(ctx, json.RawMessage(`[]`)); return err }},
		{name: "invalid call parameters", call: func() error {
			response := server.callTool(ctx, rpcRequest{ID: json.RawMessage(`1`), Params: json.RawMessage(`[]`)})
			if response.Error == nil {
				return nil
			}
			return errors.New("RPC error observed")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func encodeArguments(t *testing.T, values map[string]any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

type testResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func decodeResponses(t *testing.T, content []byte) []testResponse {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(content))
	var responses []testResponse
	for {
		var response testResponse
		if err := decoder.Decode(&response); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode response: %v, output = %s", err, content)
		}
		responses = append(responses, response)
	}
	return responses
}

func openMCPTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"), storage.ProjectMetadata{
		InstanceID: "mcp-test", Name: "mcp-test", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
