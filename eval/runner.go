package eval

import (
	"agent-demo/agent"
	"context"
	"sync"
	"time"
)

// Service 用例生成 + 自动执行 + 打分
type Service struct {
	Agent *agent.ToolAgent
	mu    sync.Mutex
	Cases []Case
	Last  *Report
}

func NewService(a *agent.ToolAgent) *Service {
	return &Service{Agent: a}
}

func (s *Service) SetCases(cases []Case) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cases = cases
}

func (s *Service) GetCases() []Case {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Case, len(s.Cases))
	copy(out, s.Cases)
	return out
}

func (s *Service) GetReport() *Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Last
}

// Run 顺序执行用例。onResult 用于 SSE 推送，可为 nil。
func (s *Service) Run(ctx context.Context, cases []Case, onResult func(CaseResult)) *Report {
	if len(cases) == 0 {
		cases = s.GetCases()
	}
	results := make([]CaseResult, 0, len(cases))
	for _, tc := range cases {
		if ctx.Err() != nil {
			cr := CaseResult{Case: tc, Verdict: "Blocked", Reasons: []string{"评测被取消"}}
			results = append(results, cr)
			if onResult != nil {
				onResult(cr)
			}
			continue
		}
		repeat := tc.Repeat
		if repeat <= 0 {
			repeat = 1
		}
		var last CaseResult
		for i := 0; i < repeat; i++ {
			last = s.runOne(ctx, tc)
			if last.Verdict == "Fail" || last.Verdict == "Blocked" {
				break
			}
		}
		results = append(results, last)
		if onResult != nil {
			onResult(last)
		}
	}
	report := &Report{Summary: summarize(results), Results: results}
	s.mu.Lock()
	s.Last = report
	if len(cases) > 0 {
		s.Cases = cases
	}
	s.mu.Unlock()
	return report
}

func (s *Service) runOne(ctx context.Context, tc Case) CaseResult {
	timeout := 20 * time.Second
	if tc.MaxLatency > 0 {
		timeout = time.Duration(tc.MaxLatency*1000)*time.Millisecond + 5*time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	out, err := s.Agent.RunDetailed(runCtx, tc.Input)
	obs := Observation{LatencyMs: time.Since(start).Milliseconds()}
	if out != nil {
		obs.Reply = out.Reply
		obs.ToolCalls = out.ToolCalls
	}
	if err != nil {
		obs.Error = err.Error()
		if obs.Reply == "" {
			obs.Reply = err.Error()
		}
	}
	return Score(tc, obs)
}
