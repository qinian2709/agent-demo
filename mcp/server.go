package mcp

import (
	"agent-demo/trace"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// HandleSelfHostedMCP 符合 MCP 2025-03-26 Streamable HTTP 规范的自研 MCP Server 处理器
// 核心能力：从 JSON-RPC params._meta.traceparent（或 HTTP Header）提取上游 Agent 的 TraceContext，
// 实现 Agent Client -> 自研 MCP Server -> 内部 DB / 下游 RPC 的完整跨进程调用链串联！
func HandleSelfHostedMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var rpcReq JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&rpcReq); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch rpcReq.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", fmt.Sprintf("eeo-mcp-%d", time.Now().UnixNano()))
		writeRPCResult(w, rpcReq.ID, map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{"listChanged": false},
			},
			"serverInfo": map[string]string{
				"name":    "eeo-order-mcp-server",
				"version": "1.0.0",
			},
		})

	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)

	case "tools/list":
		writeRPCResult(w, rpcReq.ID, map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "inspect_order",
					"description": "核查课程订单的支付流水与 ClassIn 排课权益发放状态（自研 MCP 工具，支持全链路 Trace 透传）",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"order_id": map[string]string{
								"type":        "string",
								"description": "订单号，例如 ORD-20261103 或 ORD-ERROR",
							},
						},
						"required": []string{"order_id"},
					},
				},
			},
		})

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
			writeRPCError(w, rpcReq.ID, -32602, "Invalid params")
			return
		}

		// 关键步骤：优先从 MCP JSON-RPC 的 params._meta 提取 traceparent（兼容 Stdio / HTTP），其次从 HTTP Header 提取
		ctx := r.Context()
		if len(params.Meta) > 0 {
			ctx = trace.ExtractTraceContext(ctx, params.Meta)
		} else {
			ctx = trace.ExtractTraceContextFromHTTP(ctx, r.Header)
		}

		result, err := executeSelfHostedTool(ctx, params.Name, params.Arguments)
		if err != nil {
			// 按 MCP 规范返回 isError: true
			writeRPCResult(w, rpcReq.ID, CallToolResult{
				IsError: true,
				Content: []ToolContent{{Type: "text", Text: err.Error()}},
			})
			return
		}

		writeRPCResult(w, rpcReq.ID, CallToolResult{
			IsError: false,
			Content: []ToolContent{{Type: "text", Text: result}},
		})

	default:
		writeRPCError(w, rpcReq.ID, -32601, "Method not found: "+rpcReq.Method)
	}
}

func executeSelfHostedTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	spanName := fmt.Sprintf("mcp.server.handle: %s", toolName)
	return trace.Trace(ctx, spanName, args, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		if toolName != "inspect_order" {
			return "", fmt.Errorf("未知的自研 MCP 工具: %s", toolName)
		}

		orderID, _ := args["order_id"].(string)
		orderID = strings.TrimSpace(orderID)
		if orderID == "" {
			orderID = "ORD-20261103"
		}

		// 子步骤 1：模拟 MCP Server 内部查询订单 MySQL 数据库
		orderInfo, err := trace.Trace(ctx, "db.query: eo_classinshop_orders", fmt.Sprintf("SELECT * FROM orders WHERE order_no='%s'", orderID), func(ctx context.Context, sql string) (string, error) {
			time.Sleep(45 * time.Millisecond)
			if strings.Contains(strings.ToUpper(orderID), "ERR") || strings.Contains(orderID, "异常") {
				return "", fmt.Errorf("SQL ErrNoRows: 订单 %s 不存在或已被风控冻结", orderID)
			}
			return fmt.Sprintf("订单号=%s | 实付=¥299.00 | 状态=PAID | 课程=《AI Agent 全栈测试实战》", orderID), nil
		}, trace.WithAttributes(
			attribute.String("openinference.span.kind", "CHAIN"),
			attribute.String("db.system", "mysql"),
			attribute.String("db.name", "eo_classinshop"),
		))
		if err != nil {
			return "", err
		}

		// 子步骤 2：模拟 MCP Server 内部调用下游 ClassIn 排课权益 RPC
		deliveryInfo, err := trace.Trace(ctx, "rpc.call: classin.verify_course_delivery", map[string]string{"order_no": orderID}, func(ctx context.Context, _ map[string]string) (string, error) {
			time.Sleep(85 * time.Millisecond)
			return "权益状态=已发放 | ClassIn 课节ID=CLS-889021 | 学员已入班", nil
		}, trace.WithAttributes(
			attribute.String("openinference.span.kind", "TOOL"),
			attribute.String("rpc.system", "grpc"),
			attribute.String("rpc.service", "classin.course.DeliveryService"),
		))
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("[自研MCP核查完成]\n1. 订单库记录：%s\n2. 排课系统核验：%s", orderInfo, deliveryInfo), nil
	}, trace.WithAttributes(
		attribute.String("openinference.span.kind", "CHAIN"),
		attribute.String("mcp.server.role", "server"),
		attribute.String("mcp.server.name", "eeo-order-mcp"),
	))
}

func writeRPCResult(w http.ResponseWriter, id interface{}, result interface{}) {
	raw, _ := json.Marshal(result)
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	})
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	})
}
