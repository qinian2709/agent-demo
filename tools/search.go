package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SearchTool 真实搜索工具（使用 DuckDuckGo）
type SearchTool struct{}

func (s *SearchTool) Name() string { return "search" }

func (s *SearchTool) Description() string {
	return "搜索通用知识或资讯，参数为搜索关键词（例如 Go 语言最新特性）"
}

func (s *SearchTool) Execute(ctx context.Context, args string) (string, error) {
	// 使用 DuckDuckGo 搜索
	results, err := duckDuckGoSearch(ctx, args)
	if err != nil {
		return "", fmt.Errorf("搜索失败: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("搜索 '%s' 没有找到结果", args), nil
	}

	// 格式化结果
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("搜索 '%s' 的结果：\n\n", args))
	for i, r := range results {
		if i >= 5 { // 只显示前 5 条
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		sb.WriteString(fmt.Sprintf("   链接：%s\n\n", r.URL))
	}

	return sb.String(), nil
}

type searchResult struct {
	Title   string `json:"Title"`
	Snippet string `json:"Text"`
	URL     string `json:"FirstURL"`
}

type duckDuckGoResponse struct {
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"RelatedTopics"`
}

func duckDuckGoSearch(ctx context.Context, query string) ([]searchResult, error) {
	// DuckDuckGo Instant Answer API
	apiURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result duckDuckGoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var results []searchResult
	for _, topic := range result.RelatedTopics {
		if topic.Text != "" {
			results = append(results, searchResult{
				Title:   topic.Text[:min(50, len(topic.Text))],
				Snippet: topic.Text,
				URL:     topic.FirstURL,
			})
		}
	}

	return results, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
