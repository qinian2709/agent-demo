// Package llm 提供真实的 LLM 客户端（OpenAI 兼容 API），支持超时自动重试与备用模型降级
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client LLM 客户端接口
type Client interface {
	Chat(ctx context.Context, prompt string) (string, int, error)
	ModelName() string
}

// OpenAIClient 真实的 OpenAI 兼容 API 客户端
type OpenAIClient struct {
	BaseURL        string
	APIKey         string
	Model          string
	FallbackModels []string
}

// NewOpenAIClient 创建新的 OpenAI 客户端
func NewOpenAIClient(baseURL, apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		BaseURL:        baseURL,
		APIKey:         apiKey,
		Model:          model,
		FallbackModels: []string{"dashscope/qwen3.7-plus", "kimi-k2.6", "tokenhub/glm-5.2"},
	}
}

func (c *OpenAIClient) ModelName() string {
	return c.Model
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	EnableThinking  *bool         `json:"enable_thinking,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *OpenAIClient) Chat(ctx context.Context, prompt string) (string, int, error) {
	candidates := []string{c.Model}
	for _, fb := range c.FallbackModels {
		if fb != "" && fb != c.Model {
			candidates = append(candidates, fb)
		}
	}

	var lastErr error
	for i, model := range candidates {
		// 单次请求限时 15 秒，避免单个上游节点卡住 60 秒导致网关超时
		timeout := 15 * time.Second
		if i > 0 {
			timeout = 18 * time.Second
		}
		reply, tokens, err := c.chatOnce(ctx, model, prompt, timeout)
		if err == nil && strings.TrimSpace(reply) != "" {
			return reply, tokens, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("empty response from model %s", model)
		}
		// 如果是调用方主动取消（如浏览器关闭连接），不再继续重试
		if ctx.Err() != nil {
			return "", 0, lastErr
		}
	}
	return "", 0, lastErr
}

func (c *OpenAIClient) chatOnce(ctx context.Context, model, prompt string, timeout time.Duration) (string, int, error) {
	disableThinking := false
	reqBody := chatRequest{
		Model:           model,
		EnableThinking:  &disableThinking,
		ReasoningEffort: "none",
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("marshal request: %w", err)
	}

	// 注意：c.BaseURL 需要拼接 /v1/chat/completions（兼容 BaseURL 带或不带 /v1）
	endpoint := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	endpoint += "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("do request (%s): %w", model, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("API error (%s): %s - %s", model, resp.Status, string(body))
	}

	var result chatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", 0, fmt.Errorf("no choices in response")
	}

	content := result.Choices[0].Message.Content
	if content == "" && result.Choices[0].Message.ReasoningContent != "" {
		content = result.Choices[0].Message.ReasoningContent
	}
	return content, result.Usage.TotalTokens, nil
}
