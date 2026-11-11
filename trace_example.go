//go:build ignore

// 示例：如何复用 X-Request-ID 作为 trace ID
//
// 场景 1：从 HTTP 请求自动提取 X-Request-ID
// 场景 2：手动指定 request ID
// 场景 3：在 Agent 中使用

package main

import (
	"agent-demo/trace"
	"context"
	"fmt"
	"net/http"
)

func main() {
	// 初始化 tracer
	tp, _ := trace.InitTracer("example-service")
	if tp != nil {
		defer tp.Shutdown(context.Background())
	}

	// ===== 场景 1：从 HTTP 请求自动提取 X-Request-ID =====
	example1_FromHTTPRequest()

	// ===== 场景 2：手动指定 request ID =====
	example2_ManualRequestID()

	// ===== 场景 3：在 Gin handler 中使用 =====
	example3_GinHandler()
}

// 场景 1：从 HTTP 请求自动提取 X-Request-ID
func example1_FromHTTPRequest() {
	fmt.Println("\n===== 场景 1：从 HTTP 请求自动提取 X-Request-ID =====")

	// 模拟 HTTP 请求（带 X-Request-ID）
	req, _ := http.NewRequest("GET", "http://localhost/api/chat", nil)
	req.Header.Set("X-Request-ID", "req-abc123def456")

	// 从请求创建 trace context（自动提取 X-Request-ID 并转换为 trace ID）
	ctx := trace.ContextFromRequest(context.Background(), req)

	fmt.Printf("X-Request-ID: %s\n", req.Header.Get("X-Request-ID"))
	fmt.Printf("Trace ID: %s\n", trace.GetTraceID(ctx))

	// 后续所有 span 都会复用这个 trace ID
	_, err := trace.TraceAgent(ctx, "MyAgent", "用户问题", func(ctx context.Context) (string, error) {
		fmt.Printf("  Agent Trace ID: %s\n", trace.GetTraceID(ctx))
		return "回复", nil
	})
	if err != nil {
		fmt.Println("Error:", err)
	}
}

// 场景 2：手动指定 request ID
func example2_ManualRequestID() {
	fmt.Println("\n===== 场景 2：手动指定 request ID =====")

	requestID := "my-custom-request-id-789"

	// 从 request ID 创建 trace context
	ctx := trace.ContextFromRequestID(context.Background(), requestID)

	fmt.Printf("Request ID: %s\n", requestID)
	fmt.Printf("Trace ID: %s\n", trace.GetTraceID(ctx))

	// 使用这个 context
	_, err := trace.TraceAgent(ctx, "MyAgent", "用户问题", func(ctx context.Context) (string, error) {
		fmt.Printf("  Agent Trace ID: %s\n", trace.GetTraceID(ctx))
		return "回复", nil
	})
	if err != nil {
		fmt.Println("Error:", err)
	}
}

// 场景 3：在 Gin handler 中使用
func example3_GinHandler() {
	fmt.Println("\n===== 场景 3：在 Gin handler 中使用 =====")
	fmt.Println("Gin handler 示例代码：")
	fmt.Println(`
func (h *ChatHandler) Chat(c *gin.Context) {
    // 1. 从请求创建 trace context（自动提取 X-Request-ID）
    ctx := trace.ContextFromRequest(c.Request.Context(), c.Request)

    // 2. 使用这个 context 调用 Agent
    result, err := h.agent.Run(ctx, input)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // 3. 响应头带上 request ID（可选，方便客户端排查）
    c.Header("X-Request-ID", trace.GetTraceID(ctx))

    c.JSON(200, gin.H{"result": result})
}
`)
}
