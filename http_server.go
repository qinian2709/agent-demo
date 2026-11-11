package main

import (
	"agent-demo/agent"
	"agent-demo/eval"
	"agent-demo/llm"
	"agent-demo/mcp"
	"agent-demo/tools"
	"agent-demo/trace"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type ChatRequest struct {
	Message string `json:"message"`
}

type ChatResponse struct {
	Reply      string         `json:"reply"`
	ToolCalls  []ToolCallInfo `json:"tool_calls,omitempty"`
	LatencyMs  int64          `json:"latency_ms"`
	TraceID    string         `json:"trace_id"`
	PhoenixURL string         `json:"phoenix_url,omitempty"`
}

type ToolCallInfo struct {
	Name      string `json:"name"`
	Args      string `json:"args"`
	Result    string `json:"result,omitempty"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

const defaultPhoenixURL = "https://agent-phoenix.coolify.eeo-inc.com"

var toolAgent *agent.ToolAgent
var evalService *eval.Service

func main() {
	ctx := context.Background()

	// 1. 初始化 trace（传入当前项目名称，Phoenix 会自动按此名称归类到 Projects 列表）
	tp, err := trace.InitTracer("agent-test-platform")
	if err != nil {
		log.Fatal(err)
	}
	if tp != nil {
		defer tp.Shutdown(ctx)
	}

	// 2. 创建真实的 LLM 客户端（支持通过 LLM_MODEL 环境变量切换模型，默认使用快速稳定的 dashscope/qwen3.7-plus）
	llmModel := os.Getenv("LLM_MODEL")
	if llmModel == "" {
		llmModel = "dashscope/qwen3.7-plus"
	}
	llmBaseURL := os.Getenv("LLM_BASE_URL")
	if llmBaseURL == "" {
		llmBaseURL = "https://llm-gw-lab.eeo-inc.com"
	}
	llmClient := llm.NewOpenAIClient(
		llmBaseURL,
		os.Getenv("LLM_API_KEY"),
		llmModel,
	)

	// 3. 确定端口与 Phoenix 托管站地址
	port := "3000"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}
	phoenixBaseURL := os.Getenv("PHOENIX_ENDPOINT")
	if phoenixBaseURL == "" {
		phoenixBaseURL = defaultPhoenixURL
	}

	// 4. 注册真实工具（本地工具 + 方案1第三方现成MCP + 方案2自研全链路透传MCP）
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(&tools.SearchTool{})
	toolRegistry.Register(&tools.CalculatorTool{})
	toolRegistry.Register(&tools.WeatherTool{})
	toolRegistry.Register(tools.NewExternalPhoenixMCPTool(phoenixBaseURL))
	toolRegistry.Register(tools.NewSelfHostedOrderMCPTool(port))

	// 5. 创建 Agent 与评测服务
	toolAgent = agent.NewToolAgent(llmClient, toolRegistry)
	evalService = eval.NewService(toolAgent)

	// 6. HTTP 路由
	mux := http.NewServeMux()

	sourceDir := "."
	if _, err := os.Stat("chat.html"); err != nil {
		if _, filename, _, ok := runtime.Caller(0); ok {
			sourceDir = filepath.Dir(filename)
		}
	}

	fileServer := http.FileServer(http.Dir(sourceDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/eval.html", http.StatusFound)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/chat", handleChat)
	mux.HandleFunc("/mcp/self-hosted", mcp.HandleSelfHostedMCP)
	registerEvalRoutes(mux, evalService)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"model":           llmModel,
			"tracing_enabled": trace.IsTracingEnabled(),
			"phoenix_url":     phoenixBaseURL,
			"app_env":         os.Getenv("APP_ENV"),
			"mcp_servers": []string{
				"phoenix-official-mcp (" + phoenixBaseURL + "/mcp)",
				"eeo-order-mcp (/mcp/self-hosted)",
			},
		})
	})

	// 7. 启动服务
	fmt.Printf("\n  🧪 Agent 测试评测台已启动\n")
	fmt.Printf("  → 评测: http://localhost:%s/eval.html\n", port)
	fmt.Printf("  → 对话沙箱: http://localhost:%s/chat.html\n", port)
	fmt.Printf("  → 生成用例: POST /api/eval/generate\n")
	fmt.Printf("  → 执行评测: POST /api/eval/run\n\n")

	if trace.IsTracingEnabled() {
		fmt.Printf("  📊 Trace (Phoenix): %s\n\n", phoenixBaseURL)
	}

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// 从 X-Request-ID 创建（或自动生成）trace context
	ctx := trace.ContextFromRequest(r.Context(), r)
	requestID := r.Header.Get("X-Request-ID")

	start := time.Now()

	// 调用 Agent（内部通过 OpenTelemetry SDK 自动埋点并异步上报 Phoenix）
	result, err := toolAgent.RunDetailed(ctx, req.Message)
	latency := time.Since(start).Milliseconds()

	w.Header().Set("Content-Type", "application/json")
	if requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       err.Error(),
			"latency_ms":  latency,
			"trace_id":    trace.GetTraceID(ctx),
			"phoenix_url": defaultPhoenixURL,
		})
		return
	}

	resp := ChatResponse{
		LatencyMs:  latency,
		TraceID:    trace.GetTraceID(ctx),
		PhoenixURL: defaultPhoenixURL,
	}
	if result != nil {
		resp.Reply = result.Reply
		for _, tc := range result.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, ToolCallInfo{
				Name:      tc.Name,
				Args:      tc.Args,
				Result:    tc.Result,
				Status:    tc.Status,
				LatencyMs: tc.LatencyMs,
			})
		}
	}
	json.NewEncoder(w).Encode(resp)
}
