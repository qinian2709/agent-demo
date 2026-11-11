//go:build ignore

package main

import (
	"agent-demo/agent"
	"agent-demo/llm"
	"agent-demo/tools"
	"agent-demo/trace"
	"context"
	"fmt"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	// 1. 初始化 trace
	tp, err := trace.InitTracer("agent-demo")
	if err != nil {
		log.Fatal(err)
	}
	if tp != nil {
		defer tp.Shutdown(ctx)
	}

	// 2. 创建真实的 LLM 客户端（使用 cc-switch 的配置）
	llmBaseURL := os.Getenv("LLM_BASE_URL")
	if llmBaseURL == "" {
		llmBaseURL = "https://llm-gw-lab.eeo-inc.com"
	}
	llmClient := llm.NewOpenAIClient(
		llmBaseURL,
		os.Getenv("LLM_API_KEY"),
		"deepseek-v4-pro",
	)

	// 3. 注册真实工具
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(&tools.SearchTool{})     // 真实搜索工具
	toolRegistry.Register(&tools.CalculatorTool{}) // 真实计算器工具
	toolRegistry.Register(&tools.WeatherTool{})    // 真实天气工具

	fmt.Printf("已注册工具：%v\n\n", toolRegistry.List())

	// 4. 创建 Agent
	toolAgent := agent.NewToolAgent(llmClient, toolRegistry)

	// 5. 运行真实测试用例
	testCases := []string{
		"帮我搜索一下 Go 语言的最新特性",
		"计算 123 * 456 等于多少",
		"北京今天天气怎么样",
		"上海天气如何，需要带伞吗",
	}

	for _, input := range testCases {
		fmt.Printf("===== 输入：%s =====\n", input)
		output, err := toolAgent.Run(ctx, input)
		if err != nil {
			fmt.Printf("❌ 错误：%v\n\n", err)
			continue
		}
		fmt.Printf("✅ 输出：%s\n\n", output)
	}

	fmt.Println("===== 测试完成 =====")
	fmt.Println("打开 http://localhost:6006 查看 trace 详情")
	fmt.Println("（设置 ENABLE_TRACING=true 启用 trace）")

	// 保持运行，让用户查看 Phoenix Web UI
	if os.Getenv("ENABLE_TRACING") == "true" {
		fmt.Println("\n按 Ctrl+C 退出...")
		select {}
	}
}
