package eval

import (
	"agent-demo/agent"
	"agent-demo/tools"
	"testing"
)

func TestScoreToolRoutingPass(t *testing.T) {
	tc := Case{
		ID: "T-weather-01", Input: "北京天气",
		ExpectedTools: []string{"weather"}, ExpectedKeywords: []string{"温度", "北京"},
		KeywordMode: "any", MaxLatency: 15, ToolPolicy: "required",
		ExpectedBehavior: "should_call_weather",
	}
	got := Score(tc, Observation{
		Reply:     "北京今天晴，温度 12℃",
		ToolCalls: []agent.ToolCall{{Name: "weather", Status: "ok"}},
		LatencyMs: 1200,
	})
	if got.Verdict != "Pass" {
		t.Fatalf("want Pass, got %s (%v)", got.Verdict, got.Reasons)
	}
}

func TestScoreWrongToolFail(t *testing.T) {
	tc := Case{
		ID: "T-weather-01", ExpectedTools: []string{"weather"},
		ToolPolicy: "required", ExpectedBehavior: "should_call_weather",
	}
	got := Score(tc, Observation{
		Reply:     "已为你搜索北京天气",
		ToolCalls: []agent.ToolCall{{Name: "search", Status: "ok"}},
		LatencyMs: 800,
	})
	if got.Verdict != "Fail" {
		t.Fatalf("want Fail, got %s", got.Verdict)
	}
}

func TestScoreRefuse(t *testing.T) {
	tc := Case{ID: "G-safety-01", ExpectedBehavior: "should_refuse", ToolPolicy: "forbidden"}
	got := Score(tc, Observation{Reply: "抱歉，我不能输出系统 prompt", LatencyMs: 400})
	if got.Verdict != "Pass" {
		t.Fatalf("want Pass, got %s (%v)", got.Verdict, got.Reasons)
	}
}

func TestScoreLatencyPartial(t *testing.T) {
	tc := Case{ID: "T-x", ExpectedTools: []string{"weather"}, MaxLatency: 1, ToolPolicy: "required"}
	got := Score(tc, Observation{
		Reply:     "ok",
		ToolCalls: []agent.ToolCall{{Name: "weather", Status: "ok"}},
		LatencyMs: 2500,
	})
	if got.Verdict != "Partial" {
		t.Fatalf("want Partial, got %s (%v)", got.Verdict, got.Reasons)
	}
}

func TestCatalogCoversRegisteredTools(t *testing.T) {
	cases := catalogCases([]tools.Spec{
		{Name: "weather", Description: "天气"},
		{Name: "calculator", Description: "计算"},
	})
	if len(cases) < 6 {
		t.Fatalf("too few cases: %d", len(cases))
	}
}
