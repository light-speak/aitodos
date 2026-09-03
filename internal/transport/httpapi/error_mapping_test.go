package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/storage"
)

type errorMappingCase struct {
	name   string
	err    error
	status int
	code   string
}

func TestAPIErrorMappings(t *testing.T) {
	tests := []struct {
		name   string
		writer func(http.ResponseWriter, error)
		cases  []errorMappingCase
	}{
		{name: "agent profile", writer: writeAgentProfileError, cases: []errorMappingCase{
			{name: "not found", err: storage.ErrAgentProfileNotFound, status: http.StatusNotFound, code: "AGENT_PROFILE_NOT_FOUND"},
			{name: "revision conflict", err: storage.ErrAgentProfileRevisionConflict, status: http.StatusConflict, code: "AGENT_PROFILE_REVISION_CONFLICT"},
			{name: "invalid", err: errors.New("invalid profile"), status: http.StatusBadRequest, code: "INVALID_AGENT_PROFILE"},
		}},
		{name: "capability", writer: writeCapabilityError, cases: []errorMappingCase{
			{name: "not found", err: storage.ErrCapabilityNotFound, status: http.StatusNotFound, code: "CAPABILITY_NOT_FOUND"},
			{name: "conflict", err: storage.ErrCapabilityConflict, status: http.StatusConflict, code: "CAPABILITY_CONFLICT"},
			{name: "invalid", err: errors.New("invalid capability"), status: http.StatusBadRequest, code: "INVALID_CAPABILITY"},
		}},
		{name: "clarification", writer: writeClarificationError, cases: []errorMappingCase{
			{name: "not found", err: storage.ErrClarificationNotFound, status: http.StatusNotFound, code: "CLARIFICATION_NOT_FOUND"},
			{name: "conflict", err: storage.ErrClarificationConflict, status: http.StatusConflict, code: "CLARIFICATION_CONFLICT"},
			{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
			{name: "topic", err: storage.ErrTopicNotFound, status: http.StatusNotFound, code: "TOPIC_NOT_FOUND"},
			{name: "invalid", err: errors.New("invalid answer"), status: http.StatusBadRequest, code: "INVALID_CLARIFICATION_ANSWER"},
		}},
		{name: "knowledge", writer: writeKnowledgeError, cases: []errorMappingCase{
			{name: "topic", err: storage.ErrTopicNotFound, status: http.StatusNotFound, code: "TOPIC_NOT_FOUND"},
			{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
			{name: "label", err: storage.ErrLabelNotFound, status: http.StatusNotFound, code: "LABEL_NOT_FOUND"},
			{name: "decision", err: storage.ErrDecisionNotFound, status: http.StatusNotFound, code: "DECISION_NOT_FOUND"},
			{name: "summary", err: storage.ErrRunSummaryNotFound, status: http.StatusNotFound, code: "RUN_SUMMARY_NOT_FOUND"},
			{name: "subject mismatch", err: storage.ErrDecisionSubjectMismatch, status: http.StatusConflict, code: "DECISION_SUBJECT_MISMATCH"},
			{name: "invalid", err: errors.New("invalid knowledge"), status: http.StatusBadRequest, code: "KNOWLEDGE_COMMAND_FAILED"},
		}},
		{name: "plan", writer: writePlanError, cases: []errorMappingCase{
			{name: "plan", err: storage.ErrPlanNotFound, status: http.StatusNotFound, code: "PLAN_NOT_FOUND"},
			{name: "topic", err: storage.ErrTopicNotFound, status: http.StatusNotFound, code: "TOPIC_NOT_FOUND"},
			{name: "plan conflict", err: storage.ErrPlanConflict, status: http.StatusConflict, code: "PLAN_CONFLICT"},
			{name: "topic conflict", err: storage.ErrTopicVersionConflict, status: http.StatusConflict, code: "PLAN_CONFLICT"},
			{name: "invalid", err: errors.New("invalid plan"), status: http.StatusBadRequest, code: "INVALID_PLAN"},
		}},
		{name: "task", writer: writeTaskError, cases: []errorMappingCase{
			{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
			{name: "feedback", err: storage.ErrTaskFeedbackNotFound, status: http.StatusNotFound, code: "TASK_FEEDBACK_NOT_FOUND"},
			{name: "topic", err: storage.ErrTopicNotFound, status: http.StatusNotFound, code: "TOPIC_NOT_FOUND"},
			{name: "version", err: storage.ErrTaskVersionConflict, status: http.StatusConflict, code: "TASK_VERSION_CONFLICT"},
			{name: "answer", err: storage.ErrTaskRetryRequiresAnswer, status: http.StatusConflict, code: "CLARIFICATION_ANSWER_REQUIRED"},
			{name: "self link", err: storage.ErrSelfTaskLink, status: http.StatusBadRequest, code: "INVALID_RELATION"},
			{name: "feedback conflict", err: storage.ErrTaskFeedbackConflict, status: http.StatusConflict, code: "TASK_FEEDBACK_CONFLICT"},
			{name: "archive", err: storage.ErrTaskArchiveState, status: http.StatusConflict, code: "TASK_COMMAND_CONFLICT"},
			{name: "edit", err: storage.ErrTaskEditState, status: http.StatusConflict, code: "TASK_COMMAND_CONFLICT"},
			{name: "transition", err: &task.TransitionError{Current: task.StatusAccepted, Command: task.CommandClaimRun}, status: http.StatusConflict, code: "INVALID_TASK_TRANSITION"},
			{name: "internal", err: errors.New("database"), status: http.StatusInternalServerError, code: "TASK_COMMAND_FAILED"},
		}},
		{name: "topic", writer: writeTopicError, cases: []errorMappingCase{
			{name: "topic", err: storage.ErrTopicNotFound, status: http.StatusNotFound, code: "TOPIC_NOT_FOUND"},
			{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
			{name: "version", err: storage.ErrTopicVersionConflict, status: http.StatusConflict, code: "TOPIC_VERSION_CONFLICT"},
			{name: "internal", err: errors.New("database"), status: http.StatusInternalServerError, code: "TOPIC_COMMAND_FAILED"},
		}},
		{name: "git", writer: writeGitWorkflowError, cases: gitWorkflowErrorCases()},
	}
	for _, group := range tests {
		t.Run(group.name, func(t *testing.T) {
			assertErrorMappings(t, group.writer, group.cases)
		})
	}
}

func TestDelegatingAPIErrorMappings(t *testing.T) {
	assertErrorMappings(t, writeAssessmentError, []errorMappingCase{
		{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
		{name: "internal", err: errors.New("database"), status: http.StatusInternalServerError, code: "ASSESSMENT_READ_FAILED"},
	})
	assertErrorMappings(t, writeQualityError, []errorMappingCase{
		{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
		{name: "invalid", err: errors.New("invalid quality"), status: http.StatusBadRequest, code: "INVALID_QUALITY_DATA"},
	})
	assertErrorMappings(t, writeRunReadError, []errorMappingCase{
		{name: "run", err: storage.ErrRunNotFound, status: http.StatusNotFound, code: "RUN_NOT_FOUND"},
		{name: "artifact", err: storage.ErrRunArtifactNotFound, status: http.StatusNotFound, code: "RUN_NOT_FOUND"},
		{name: "internal", err: errors.New("database"), status: http.StatusInternalServerError, code: "RUN_READ_FAILED"},
	})
}

func gitWorkflowErrorCases() []errorMappingCase {
	return []errorMappingCase{
		{name: "task", err: storage.ErrTaskNotFound, status: http.StatusNotFound, code: "TASK_NOT_FOUND"},
		{name: "release", err: storage.ErrReleaseConflict, status: http.StatusConflict, code: "RELEASE_CONFLICT"},
		{name: "tag", err: gitworkflow.ErrTagConflict, status: http.StatusConflict, code: "RELEASE_CONFLICT"},
		{name: "identity", err: gitworkflow.ErrWorkspaceIdentity, status: http.StatusConflict, code: "WORKSPACE_QUARANTINED"},
		{name: "dirty", err: gitworkflow.ErrWorkspaceDirty, status: http.StatusConflict, code: "WORKSPACE_DIRTY"},
		{name: "clean", err: gitworkflow.ErrWorkspaceClean, status: http.StatusConflict, code: "WORKSPACE_CLEAN"},
		{name: "unborn", err: gitworkflow.ErrRepositoryUnborn, status: http.StatusConflict, code: "REPOSITORY_UNBORN"},
		{name: "branch invalid", err: gitworkflow.ErrTargetBranchInvalid, status: http.StatusBadRequest, code: "INVALID_TARGET_BRANCH"},
		{name: "branch missing", err: gitworkflow.ErrTargetBranchNotFound, status: http.StatusBadRequest, code: "INVALID_TARGET_BRANCH"},
		{name: "workspace exists", err: storage.ErrTaskWorkspaceExists, status: http.StatusConflict, code: "TARGET_BRANCH_LOCKED"},
		{name: "change", err: gitworkflow.ErrChangeNotFound, status: http.StatusNotFound, code: "CHANGE_NOT_FOUND"},
		{name: "version", err: storage.ErrTaskVersionConflict, status: http.StatusConflict, code: "TASK_VERSION_CONFLICT"},
		{name: "tests", err: storage.ErrRequiredTestsNotPassed, status: http.StatusConflict, code: "REQUIRED_TESTS_NOT_PASSED"},
		{name: "needs sync", err: gitworkflow.ErrTargetNeedsSync, status: http.StatusConflict, code: "TARGET_NEEDS_SYNC"},
		{name: "target busy", err: gitworkflow.ErrTargetWorktreeBusy, status: http.StatusConflict, code: "TARGET_WORKTREE_BUSY"},
		{name: "repository dirty", err: gitworkflow.ErrRepositoryDirty, status: http.StatusConflict, code: "TARGET_WORKTREE_DIRTY"},
		{name: "operation active", err: gitworkflow.ErrGitOperationActive, status: http.StatusConflict, code: "GIT_OPERATION_ACTIVE"},
		{name: "not accepted", err: gitworkflow.ErrTaskNotAccepted, status: http.StatusConflict, code: "TASK_NOT_INTEGRATABLE"},
		{name: "review commit", err: gitworkflow.ErrReviewCommitMissing, status: http.StatusConflict, code: "TASK_NOT_INTEGRATABLE"},
		{name: "review head", err: gitworkflow.ErrReviewHeadMismatch, status: http.StatusConflict, code: "TASK_NOT_INTEGRATABLE"},
		{name: "sync unnecessary", err: gitworkflow.ErrTargetSyncNotNeeded, status: http.StatusConflict, code: "TARGET_SYNC_NOT_NEEDED"},
		{name: "transition", err: &task.TransitionError{Current: task.StatusReady, Command: task.CommandAccept}, status: http.StatusConflict, code: "INVALID_TASK_TRANSITION"},
		{name: "default", err: errors.New("git failed"), status: http.StatusConflict, code: "GIT_OPERATION_FAILED"},
	}
}

func assertErrorMappings(t *testing.T, writer func(http.ResponseWriter, error), tests []errorMappingCase) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writer(recorder, test.err)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
			}
			var response errorEnvelope
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != test.code {
				t.Fatalf("code = %q, want %q", response.Error.Code, test.code)
			}
		})
	}
}
