package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestPlanApprovalCreatesTasksRelationsAndTestsAtomically(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	topics := NewTopicStore(database)
	plans := NewPlanStore(database)
	createdTopic, err := topics.Create(ctx, topic.CreateInput{Title: "实现项目搜索"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := plans.CreateRevision(ctx, createdTopic.ID, createdTopic.Version, plan.RevisionInput{
		Summary: "先建立索引，再增加搜索界面",
		Drafts: []plan.TaskDraftInput{{
			Title: "建立全文索引", Description: "索引 Topic 与 Task", Priority: 1,
			AcceptanceCriteria: "可以按关键字找到 Topic",
			TestCases:          []plan.TestCaseInput{{Title: "Topic 可检索", Required: true}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Plan.Status != plan.StatusInReview || view.Revision.Revision != 1 {
		t.Fatalf("view = %#v", view)
	}
	reviewTopic, err := topics.Get(ctx, createdTopic.ID)
	if err != nil {
		t.Fatal(err)
	}
	approved, tasks, err := plans.Approve(ctx, view.Plan.ID, plan.ReviewInput{
		ExpectedTopicVersion: reviewTopic.Version, RevisionID: view.Revision.ID, Comment: "范围合理",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Plan.Status != plan.StatusApproved || len(tasks) != 1 || tasks[0].SourcePlanTaskDraftID == "" {
		t.Fatalf("approved = %#v, tasks = %#v", approved, tasks)
	}
	linked, err := NewRelationStore(database).ListTopicTasks(ctx, createdTopic.ID)
	if err != nil || len(linked) != 1 || linked[0].Task.ID != tasks[0].ID {
		t.Fatalf("linked = %#v, err = %v", linked, err)
	}
	quality, err := NewQualityStore(database).GetTaskQuality(ctx, tasks[0].ID)
	if err != nil || len(quality.TestCases) != 1 || !quality.TestCases[0].Required {
		t.Fatalf("quality = %#v, err = %v", quality, err)
	}
	updatedTopic, err := topics.Get(ctx, createdTopic.ID)
	if err != nil || updatedTopic.Status != topic.StatusPlanned {
		t.Fatalf("topic = %#v, err = %v", updatedTopic, err)
	}
}

func TestPlanChangesCreateImmutableSecondRevision(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	topics := NewTopicStore(database)
	plans := NewPlanStore(database)
	createdTopic, _ := topics.Create(ctx, topic.CreateInput{Title: "讨论搜索"})
	first, err := plans.CreateRevision(ctx, createdTopic.ID, createdTopic.Version, validPlanInput("第一版"))
	if err != nil {
		t.Fatal(err)
	}
	reviewTopic, _ := topics.Get(ctx, createdTopic.ID)
	changed, err := plans.RequestChanges(ctx, first.Plan.ID, plan.ReviewInput{
		ExpectedTopicVersion: reviewTopic.Version, RevisionID: first.Revision.ID, Comment: "拆得不够细",
	})
	if err != nil || changed.Plan.Status != plan.StatusChangesRequested {
		t.Fatalf("changed = %#v, err = %v", changed, err)
	}
	openTopic, _ := topics.Get(ctx, createdTopic.ID)
	second, err := plans.CreateRevision(ctx, createdTopic.ID, openTopic.Version, validPlanInput("第二版"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision.Revision != 2 || second.Revision.PreviousRevisionID != first.Revision.ID {
		t.Fatalf("second = %#v", second.Revision)
	}
	if _, _, err := plans.Approve(ctx, second.Plan.ID, plan.ReviewInput{
		ExpectedTopicVersion: openTopic.Version, RevisionID: first.Revision.ID,
	}); !errors.Is(err, ErrPlanConflict) {
		t.Fatalf("Approve() error = %v, want conflict", err)
	}
}

func validPlanInput(summary string) plan.RevisionInput {
	return plan.RevisionInput{Summary: summary, Drafts: []plan.TaskDraftInput{{Title: "实现索引", Priority: 2}}}
}
