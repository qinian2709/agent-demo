package agent

import (
	"agent-demo/llm"
	"agent-demo/tools"
	"agent-demo/trace"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolAgent 工具类 Agent
type ToolAgent struct {
	llm   llm.Client
	tools *tools.Registry
}

func NewToolAgent(llmClient llm.Client, toolRegistry *tools.Registry) *ToolAgent {
	return &ToolAgent{
		llm:   llmClient,
		tools: toolRegistry,
	}
}

// RunResult 一次 Agent 执行的可断言结果（给测试评测用，不依赖日志链路）
type RunResult struct {
	Reply     string
	ToolCalls []ToolCall
}

// ToolCall 一次工具调用的观测记录
type ToolCall struct {
	Name      string `json:"name"`
	Args      string `json:"args"`
	Result    string `json:"result,omitempty"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

func (a *ToolAgent) Tools() *tools.Registry {
	return a.tools
}

func (a *ToolAgent) LLM() llm.Client {
	return a.llm
}

func (a *ToolAgent) modelName() string {
	if a.llm != nil && a.llm.ModelName() != "" {
		return a.llm.ModelName()
	}
	return "kimi-k2.6"
}

const intentPromptTemplate = `你是一个支持本地工具与 MCP (Model Context Protocol) 协议工具的智能助手。请先判断用户问题是否需要调用以下工具：
1. search：搜索通用知识或资讯，参数为搜索关键词（例如 "Go 语言最新特性"）
2. calculator：计算数学表达式，参数为形如 "123 * 456" 的空格分隔算式（运算符仅支持 + - * / ^）
3. weather：查询城市实时天气，参数为城市名（例如 "北京"、"上海"）
4. mcp_external_phoenix：【方案1：第三方现成MCP】查询或检索 Phoenix MCP Server 内置工具目录，参数为检索词（例如 "trace"、"dataset"、"prompt"）
5. mcp_self_order：【方案2：自研MCP全链路透传】核查课程订单的数据库状态与排课发货链路，参数为订单号（例如 "ORD-20261103" 或异常订单 "ORD-ERROR"）

如果需要使用工具，请只输出如下格式的 JSON（不要输出其他解释文字）：
{"tool_calls":[{"name":"工具名","args":"参数字符串"}]}

如果不需要任何工具（如普通问答、概念解释），请直接回答用户问题。

用户问题：%s`

// Run 执行 Agent 任务
func (a *ToolAgent) Run(ctx context.Context, input string) (string, error) {
	result, err := a.RunDetailed(ctx, input)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Reply, nil
}

// RunDetailed 执行 Agent 并返回工具调用观测，供自动评测断言「调没调对工具」
func (a *ToolAgent) RunDetailed(ctx context.Context, input string) (*RunResult, error) {
	model := a.modelName()
	out := &RunResult{}
	reply, err := trace.TraceAgent(ctx, "ToolAgent", input, func(ctx context.Context) (string, error) {
		intentPrompt := fmt.Sprintf(intentPromptTemplate, input)
		resp, err := trace.TraceLLM(ctx, model, intentPrompt, func(ctx context.Context) (string, int, error) {
			return a.llm.Chat(ctx, intentPrompt)
		})
		if err != nil {
			return "", fmt.Errorf("llm call failed: %w", err)
		}

		toolCalls := a.parseToolCalls(input, resp)
		if len(toolCalls) == 0 {
			out.Reply = resp
			return resp, nil
		}

		var toolResults []string
		for _, tc := range toolCalls {
			rec := ToolCall{Name: tc.name, Args: tc.args}
			start := time.Now()
			tool, ok := a.tools.Get(tc.name)
			if !ok {
				rec.Status = "error"
				rec.Result = "tool not found: " + tc.name
				rec.LatencyMs = time.Since(start).Milliseconds()
				out.ToolCalls = append(out.ToolCalls, rec)
				return "", fmt.Errorf("tool not found: %s", tc.name)
			}
			var toolResult string
			if strings.HasPrefix(tc.name, "mcp_") {
				toolResult, err = tool.Execute(ctx, tc.args)
			} else {
				toolResult, err = trace.TraceTool(ctx, tc.name, tc.args, func(ctx context.Context) (string, error) {
					return tool.Execute(ctx, tc.args)
				})
			}
			rec.LatencyMs = time.Since(start).Milliseconds()
			if err != nil {
				rec.Status = "error"
				rec.Result = clipRunes(err.Error(), 1500)
				out.ToolCalls = append(out.ToolCalls, rec)
				toolResults = append(toolResults, fmt.Sprintf("[工具 %s 执行报错]: %v", tc.name, err))
				continue
			}
			rec.Status = "ok"
			rec.Result = clipRunes(toolResult, 1500)
			out.ToolCalls = append(out.ToolCalls, rec)
			toolResults = append(toolResults, toolResult)
		}

		allResults := strings.Join(toolResults, "\n")
		finalPrompt := fmt.Sprintf("基于以下工具执行结果简洁准确地回答用户问题：\n%s\n用户问题：%s", allResults, input)
		finalResp, err := trace.TraceLLM(ctx, model, finalPrompt, func(ctx context.Context) (string, int, error) {
			return a.llm.Chat(ctx, finalPrompt)
		})
		if err != nil {
			return "", fmt.Errorf("final llm call failed: %w", err)
		}
		out.Reply = finalResp
		return finalResp, nil
	})
	if err != nil {
		return out, err
	}
	if out.Reply == "" {
		out.Reply = reply
	}
	return out, nil
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// RunWithTimeout 演示超时场景
func (a *ToolAgent) RunWithTimeout(ctx context.Context, input string) (string, error) {
	model := a.modelName()
	return trace.TraceAgent(ctx, "ToolAgent-Timeout", input, func(ctx context.Context) (string, error) {
		// 第 1 步：LLM 调用
		resp, err := trace.TraceLLM(ctx, model, input, func(ctx context.Context) (string, int, error) {
			return a.llm.Chat(ctx, input)
		})
		if err != nil {
			return "", err
		}

		// 第 2 步：调用慢速工具（设置 3 秒超时，工具实际要 5 秒）
		toolResult, err := trace.TraceToolWithTimeout(ctx, "slow_tool", "测试参数", 3*time.Second, func(ctx context.Context) (string, error) {
			tool, ok := a.tools.Get("slow_tool")
			if !ok {
				return "", fmt.Errorf("tool not found")
			}
			return tool.Execute(ctx, "测试参数")
		})
		if err != nil {
			return fmt.Sprintf("LLM 回复：%s\n工具调用超时：%v", resp, err), nil
		}

		// 第 3 步：基于工具结果再次调用 LLM
		finalPrompt := fmt.Sprintf("工具结果：%s\n用户问题：%s", toolResult, input)
		finalResp, err := trace.TraceLLM(ctx, model, finalPrompt, func(ctx context.Context) (string, int, error) {
			return a.llm.Chat(ctx, finalPrompt)
		})
		if err != nil {
			return "", err
		}

		return finalResp, nil
	})
}

// RunWithError 演示错误场景
func (a *ToolAgent) RunWithError(ctx context.Context, input string) (string, error) {
	model := a.modelName()
	return trace.TraceAgent(ctx, "ToolAgent-Error", input, func(ctx context.Context) (string, error) {
		// 第 1 步：LLM 调用
		resp, err := trace.TraceLLM(ctx, model, input, func(ctx context.Context) (string, int, error) {
			return a.llm.Chat(ctx, input)
		})
		if err != nil {
			return "", err
		}

		// 第 2 步：调用会报错的工具
		toolResult, err := trace.TraceTool(ctx, "error_tool", "无效参数", func(ctx context.Context) (string, error) {
			tool, ok := a.tools.Get("error_tool")
			if !ok {
				return "", fmt.Errorf("tool not found")
			}
			return tool.Execute(ctx, "无效参数")
		})
		if err != nil {
			return fmt.Sprintf("LLM 回复：%s\n工具执行失败：%v", resp, err), nil
		}

		// 第 3 步：基于工具结果再次调用 LLM
		finalPrompt := fmt.Sprintf("工具结果：%s\n用户问题：%s", toolResult, input)
		finalResp, err := trace.TraceLLM(ctx, model, finalPrompt, func(ctx context.Context) (string, int, error) {
			return a.llm.Chat(ctx, finalPrompt)
		})
		if err != nil {
			return "", err
		}

		return finalResp, nil
	})
}

type toolCall struct {
	Name string `json:"name"`
	Args string `json:"args"`
	name string
	args string
}

type intentDecision struct {
	ToolCalls []struct {
		Name string `json:"name"`
		Args string `json:"args"`
	} `json:"tool_calls"`
}

// parseToolCalls 优先解析 LLM 输出的结构化 JSON 工具指令；若未命中 JSON 则按用户输入意图兜底
func (a *ToolAgent) parseToolCalls(userInput, resp string) []toolCall {
	cleaned := strings.TrimSpace(resp)
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			jsonPart := cleaned[start : end+1]
			var decision intentDecision
			if err := json.Unmarshal([]byte(jsonPart), &decision); err == nil && len(decision.ToolCalls) > 0 {
				var calls []toolCall
				for _, tc := range decision.ToolCalls {
					if tc.Name != "" {
						calls = append(calls, toolCall{name: tc.Name, args: tc.Args})
					}
				}
				if len(calls) > 0 {
					return calls
				}
			}
		}
	}

	// 兜底：仅按用户的原始输入判断（不再误匹配 LLM 拒答文本中的"搜索引擎"字样）
	inputLower := strings.ToLower(userInput)
	var calls []toolCall
	if strings.Contains(inputLower, "第三方mcp") || strings.Contains(inputLower, "现成mcp") || strings.Contains(inputLower, "phoenix") {
		calls = append(calls, toolCall{name: "mcp_external_phoenix", args: "trace"})
		return calls
	}
	if strings.Contains(inputLower, "ord-") || strings.Contains(inputLower, "订单") || strings.Contains(inputLower, "自研mcp") {
		orderID := "ORD-20261103"
		if strings.Contains(strings.ToUpper(userInput), "ORD-ERROR") || strings.Contains(userInput, "异常") {
			orderID = "ORD-ERROR"
		}
		calls = append(calls, toolCall{name: "mcp_self_order", args: orderID})
		return calls
	}
	if strings.Contains(inputLower, "搜索") || strings.Contains(inputLower, "search") {
		calls = append(calls, toolCall{name: "search", args: userInput})
	}
	if strings.Contains(inputLower, "计算") || strings.Contains(inputLower, "calculate") {
		calls = append(calls, toolCall{name: "calculator", args: "123 * 456"})
	}
	if strings.Contains(inputLower, "天气") || strings.Contains(inputLower, "weather") {
		city := "北京"
		if strings.Contains(userInput, "上海") {
			city = "上海"
		}
		calls = append(calls, toolCall{name: "weather", args: city})
	}
	return calls
}
