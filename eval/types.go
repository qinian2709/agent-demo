package eval

import "agent-demo/agent"

// Case 一条可自动执行的 Agent 测试用例
type Case struct {
	ID               string   `json:"id"`
	Category         string   `json:"category"`
	Input            string   `json:"input"`
	ExpectedKeywords []string `json:"expected_keywords,omitempty"`
	KeywordMode      string   `json:"keyword_mode,omitempty"` // any | all，默认 any
	ExpectedTools    []string `json:"expected_tools,omitempty"`
	ForbiddenTools   []string `json:"forbidden_tools,omitempty"`
	ExpectedBehavior string   `json:"expected_behavior,omitempty"`
	MaxLatency       float64  `json:"max_latency"`
	ToolPolicy       string   `json:"tool_policy,omitempty"` // required | forbidden | optional
	Repeat           int      `json:"repeat,omitempty"`
	Risk             string   `json:"risk,omitempty"`
	Rubric           string   `json:"rubric,omitempty"`
}

// Observation 一次真实执行的观测
type Observation struct {
	Reply     string           `json:"reply"`
	ToolCalls []agent.ToolCall `json:"tool_calls"`
	LatencyMs int64            `json:"latency_ms"`
	Error     string           `json:"error,omitempty"`
}

// CaseResult 单条用例的评测结果
type CaseResult struct {
	Case        Case        `json:"case"`
	Observation Observation `json:"observation"`
	Verdict     string      `json:"verdict"` // Pass | Partial | Fail | Blocked
	Reasons     []string    `json:"reasons"`
	Checks      []Check     `json:"checks"`
}

// Check 单项断言
type Check struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"` // fail | warn
}

// Report 一次评测报告
type Report struct {
	Summary Summary      `json:"summary"`
	Results []CaseResult `json:"results"`
}

// Summary 汇总
type Summary struct {
	Total    int     `json:"total"`
	Pass     int     `json:"pass"`
	Partial  int     `json:"partial"`
	Fail     int     `json:"fail"`
	Blocked  int     `json:"blocked"`
	PassRate float64 `json:"pass_rate"`
}

// GenerateRequest 生成用例请求
type GenerateRequest struct {
	UseLLM bool   `json:"use_llm"`
	Focus  string `json:"focus"`
}

// GenerateResponse 生成用例响应
type GenerateResponse struct {
	Cases  []Case `json:"cases"`
	Source string `json:"source"`
}

// RunRequest 执行评测请求
type RunRequest struct {
	Cases    []Case `json:"cases"`
	UseJudge bool   `json:"use_judge"`
}
