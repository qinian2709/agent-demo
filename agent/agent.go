// Package agent 提供 Agent 接口和实现
package agent

import "context"

// Agent Agent 接口
type Agent interface {
	Run(ctx context.Context, input string) (string, error)
}
