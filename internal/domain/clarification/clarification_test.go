package clarification

import "testing"

func TestRequestRequiresAnswerableOptions(t *testing.T) {
	valid := Request{
		Category: CategoryDecision,
		Question: "数据库迁移采用哪种策略？",
		Options: []Option{
			{ID: "compatible", Label: "兼容升级", Description: "保留旧数据"},
			{ID: "fresh", Label: "仅新项目", Description: "不迁移旧数据"},
		},
		RecommendedOptionID: "compatible",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}

	cases := []Request{
		{Category: CategoryDecision, Question: "没有可回答方式"},
		{Category: CategoryDecision, Question: "重复选项", Options: []Option{{ID: "same", Label: "一"}, {ID: "same", Label: "二"}}},
		{Category: CategoryDecision, Question: "错误推荐", Options: valid.Options, RecommendedOptionID: "missing"},
	}
	for _, item := range cases {
		if err := item.Validate(); err == nil {
			t.Fatalf("invalid request unexpectedly accepted: %+v", item)
		}
	}
}

func TestAnswerUsesOptionOrAllowedCustomText(t *testing.T) {
	item := Clarification{
		Options:           []Option{{ID: "yes", Label: "是"}, {ID: "no", Label: "否"}},
		AllowCustomAnswer: true,
	}
	if err := (AnswerInput{SelectedOptionID: "yes"}).ValidateFor(item); err != nil {
		t.Fatalf("option answer: %v", err)
	}
	if err := (AnswerInput{CustomAnswer: "采用分阶段迁移"}).ValidateFor(item); err != nil {
		t.Fatalf("custom answer: %v", err)
	}
	for _, answer := range []AnswerInput{
		{},
		{SelectedOptionID: "missing"},
		{SelectedOptionID: "yes", CustomAnswer: "同时填写"},
	} {
		if err := answer.ValidateFor(item); err == nil {
			t.Fatalf("invalid answer unexpectedly accepted: %+v", answer)
		}
	}
}
