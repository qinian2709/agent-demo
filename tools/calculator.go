package tools

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CalculatorTool 真实计算器工具
type CalculatorTool struct{}

func (c *CalculatorTool) Name() string { return "calculator" }

func (c *CalculatorTool) Description() string {
	return "计算数学表达式，参数为形如 123 * 456 的空格分隔算式（运算符仅支持 + - * / ^）"
}

func (c *CalculatorTool) Execute(ctx context.Context, args string) (string, error) {
	// 简单的数学表达式计算
	result, err := evaluateExpression(args)
	if err != nil {
		return "", fmt.Errorf("计算失败: %w", err)
	}
	return fmt.Sprintf("计算 '%s' 的结果：%v", args, result), nil
}

// evaluateExpression 简单的数学表达式求值
func evaluateExpression(expr string) (float64, error) {
	expr = strings.TrimSpace(expr)

	// 处理基本运算
	parts := strings.Fields(expr)
	if len(parts) == 3 {
		left, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, fmt.Errorf("无效数字: %s", parts[0])
		}

		op := parts[1]
		right, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return 0, fmt.Errorf("无效数字: %s", parts[2])
		}

		switch op {
		case "+":
			return left + right, nil
		case "-":
			return left - right, nil
		case "*":
			return left * right, nil
		case "/":
			if right == 0 {
				return 0, fmt.Errorf("除数不能为 0")
			}
			return left / right, nil
		case "^":
			return math.Pow(left, right), nil
		default:
			return 0, fmt.Errorf("未知运算符: %s", op)
		}
	}

	// 单个数字
	if val, err := strconv.ParseFloat(expr, 64); err == nil {
		return val, nil
	}

	return 0, fmt.Errorf("无法解析表达式: %s", expr)
}
