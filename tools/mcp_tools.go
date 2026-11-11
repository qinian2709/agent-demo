package tools

import (
	"agent-demo/mcp"
	"context"
	"strings"
)

// ExternalPhoenixMCPTool 方案 1：对接无代码控制权的现成第三方 MCP Server（Arize Phoenix 官方内置 MCP）
type ExternalPhoenixMCPTool struct {
	Client *mcp.Client
}

func NewExternalPhoenixMCPTool(phoenixBaseURL string) *ExternalPhoenixMCPTool {
	endpoint := strings.TrimRight(phoenixBaseURL, "/") + "/mcp"
	return &ExternalPhoenixMCPTool{
		Client: mcp.NewClient("phoenix-official-mcp", endpoint),
	}
}

func (t *ExternalPhoenixMCPTool) Name() string { return "mcp_external_phoenix" }

func (t *ExternalPhoenixMCPTool) Description() string {
	return "查询第三方 Phoenix MCP 的内置工具目录，参数为检索词（例如 trace、dataset、prompt）"
}

func (t *ExternalPhoenixMCPTool) Execute(ctx context.Context, args string) (string, error) {
	query := strings.TrimSpace(args)
	if query == "" {
		query = "trace"
	}
	return t.Client.CallTool(ctx, "search", map[string]interface{}{
		"query":  query,
		"detail": "brief",
		"limit":  5,
	})
}

// SelfHostedOrderMCPTool 方案 2：对接自研 MCP Server（通过 params._meta.traceparent 跨端透传 Trace 上下文）
type SelfHostedOrderMCPTool struct {
	Client *mcp.Client
}

func NewSelfHostedOrderMCPTool(localPort string) *SelfHostedOrderMCPTool {
	endpoint := "http://127.0.0.1:" + localPort + "/mcp/self-hosted"
	return &SelfHostedOrderMCPTool{
		Client: mcp.NewClient("eeo-order-mcp", endpoint),
	}
}

func (t *SelfHostedOrderMCPTool) Name() string { return "mcp_self_order" }

func (t *SelfHostedOrderMCPTool) Description() string {
	return "核查课程订单的数据库状态与排课发货链路，参数为订单号（例如 ORD-20261103，异常订单用 ORD-ERROR）"
}

func (t *SelfHostedOrderMCPTool) Execute(ctx context.Context, args string) (string, error) {
	orderID := strings.TrimSpace(args)
	if orderID == "" {
		orderID = "ORD-20261103"
	}
	return t.Client.CallTool(ctx, "inspect_order", map[string]interface{}{
		"order_id": orderID,
	})
}
