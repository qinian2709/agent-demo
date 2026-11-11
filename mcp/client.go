// Package mcp 实现符合 Model Context Protocol (MCP 2025-03-26 Streamable HTTP / JSON-RPC 2.0) 规范的
// 1. 通用 MCP Client（带 OpenTelemetry 埋点，支持方案 1 单侧埋点 & 方案 2 _meta.traceparent 透传）
// 2. 自研 MCP Server（从 params._meta.traceparent 恢复上下文，展示跨进程分布式全链路子 Span）
package mcp

import (
	"agent-demo/trace"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// JSONRPCRequest 标准 MCP JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse 标准 MCP JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CallToolParams MCP tools/call 参数结构（含规范预留的 _meta 字段）
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Meta      map[string]string      `json:"_meta,omitempty"`
}

// CallToolResult MCP tools/call 返回结构
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Client 标准 Streamable HTTP MCP 客户端
type Client struct {
	ServerName string
	Endpoint   string
	HTTPClient *http.Client
}

func NewClient(serverName, endpoint string) *Client {
	return &Client{
		ServerName: serverName,
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Timeout: 25 * time.Second},
	}
}

// CallTool 执行完整的 MCP 生命周期：initialize -> notifications/initialized -> tools/call
// 并在 Client 侧自动记录 OpenTelemetry Span，同时将 traceparent 注入 params._meta 与 HTTP Header
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]interface{}) (string, error) {
	spanName := fmt.Sprintf("mcp.tool_call: %s/%s", c.ServerName, toolName)
	argsJSON, _ := json.Marshal(arguments)

	return trace.Trace(ctx, spanName, string(argsJSON), func(ctx context.Context, _ string) (string, error) {
		span := oteltrace.SpanFromContext(ctx)

		// 1. MCP 握手初始化 (initialize)
		sessionID, serverInfo, err := c.initializeSession(ctx)
		if err != nil {
			return "", fmt.Errorf("mcp initialize failed: %w", err)
		}
		if serverInfo != "" {
			span.SetAttributes(attribute.String("mcp.server.info", serverInfo))
		}
		if sessionID != "" {
			span.SetAttributes(attribute.String("mcp.session_id", sessionID))
			_ = c.sendInitializedNotification(ctx, sessionID)
		}

		// 2. 注入 W3C TraceContext 到 MCP 标准 params._meta（方案 2 核心：跨进程/跨协议透传）
		meta := trace.InjectTraceContext(ctx)

		callParams := CallToolParams{
			Name:      toolName,
			Arguments: arguments,
			Meta:      meta,
		}
		paramsBytes, _ := json.Marshal(callParams)
		reqPayload := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/call",
			Params:  paramsBytes,
		}

		resp, err := c.postJSONRPC(ctx, sessionID, reqPayload)
		if err != nil {
			return "", err
		}
		if resp.Error != nil {
			return "", fmt.Errorf("mcp rpc error (%d): %s", resp.Error.Code, resp.Error.Message)
		}

		var toolRes CallToolResult
		if err := json.Unmarshal(resp.Result, &toolRes); err != nil {
			return string(resp.Result), nil
		}

		var texts []string
		for _, item := range toolRes.Content {
			if item.Text != "" {
				texts = append(texts, item.Text)
			}
		}
		outText := strings.Join(texts, "\n")

		// MCP 规范：工具业务报错时 HTTP 仍为 200，但返回 isError: true
		// 此时必须将 Span 状态标记为 Error，以便在 Phoenix 看板上直观标红告警
		span.SetAttributes(attribute.Bool("mcp.is_error", toolRes.IsError))
		if toolRes.IsError {
			span.SetStatus(codes.Error, outText)
			return outText, fmt.Errorf("mcp tool returned isError=true: %s", outText)
		}

		return outText, nil
	}, trace.WithAttributes(
		attribute.String("openinference.span.kind", "TOOL"),
		attribute.String("tool.name", fmt.Sprintf("%s.%s", c.ServerName, toolName)),
		attribute.String("mcp.server.name", c.ServerName),
		attribute.String("mcp.tool.name", toolName),
		attribute.String("mcp.endpoint", c.Endpoint),
		attribute.String("mcp.transport", "streamable-http"),
	))
}

func (c *Client) initializeSession(ctx context.Context) (string, string, error) {
	initParams := map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "agent-test-platform-mcp-client",
			"version": "1.0.0",
		},
	}
	rawParams, _ := json.Marshal(initParams)
	reqBody, _ := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  rawParams,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	sessionID := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id"))
	rpcResp, err := parseMCPHTTPBody(resp.Body)
	if err != nil {
		return sessionID, "", err
	}

	var initRes struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	_ = json.Unmarshal(rpcResp.Result, &initRes)
	info := strings.TrimSpace(initRes.ServerInfo.Name + " " + initRes.ServerInfo.Version)
	return sessionID, info, nil
}

func (c *Client) sendInitializedNotification(ctx context.Context, sessionID string) error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) postJSONRPC(ctx context.Context, sessionID string, rpcReq JSONRPCRequest) (*JSONRPCResponse, error) {
	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	// 同时注入 HTTP Header 级的 W3C TraceContext
	for k, v := range trace.InjectTraceContext(ctx) {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return parseMCPHTTPBody(resp.Body)
}

// parseMCPHTTPBody 兼容解析 MCP 的 application/json 响应与 text/event-stream (SSE) 响应
func parseMCPHTTPBody(r io.Reader) (*JSONRPCResponse, error) {
	rawBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(rawBytes))
	if trimmed == "" {
		return &JSONRPCResponse{JSONRPC: "2.0"}, nil
	}

	// 若是 SSE 格式（event: message \n data: {...}）
	if strings.HasPrefix(trimmed, "event:") || strings.Contains(trimmed, "\ndata:") {
		scanner := bufio.NewScanner(strings.NewReader(trimmed))
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "data:") {
				dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				var rpcResp JSONRPCResponse
				if err := json.Unmarshal([]byte(dataStr), &rpcResp); err == nil {
					return &rpcResp, nil
				}
			}
		}
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal([]byte(trimmed), &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal mcp response: %w (raw: %s)", err, trimmed[:min(120, len(trimmed))])
	}
	return &rpcResp, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
