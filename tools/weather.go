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

// WeatherTool 真实天气查询工具（使用 Open-Meteo API，无需 API Key）
type WeatherTool struct{}

func (w *WeatherTool) Name() string { return "weather" }

func (w *WeatherTool) Description() string {
	return "查询城市实时天气，参数为城市名（例如 北京、上海）"
}

func (w *WeatherTool) Execute(ctx context.Context, args string) (string, error) {
	// 解析城市名
	city := strings.TrimSpace(args)
	if city == "" {
		city = "北京"
	}

	// 获取城市坐标
	lat, lon, err := geocodeCity(ctx, city)
	if err != nil {
		return "", fmt.Errorf("获取城市坐标失败: %w", err)
	}

	// 获取天气数据
	weather, err := getWeather(ctx, lat, lon)
	if err != nil {
		return "", fmt.Errorf("获取天气失败: %w", err)
	}

	// 格式化结果
	result := fmt.Sprintf("%s 的天气：\n", city)
	result += fmt.Sprintf("温度：%.1f°C\n", weather.Current.Temperature)
	result += fmt.Sprintf("体感温度：%.1f°C\n", weather.Current.ApparentTemperature)
	result += fmt.Sprintf("天气状况：%s\n", weatherCodeToDescription(weather.Current.WeatherCode))
	result += fmt.Sprintf("风速：%.1f km/h\n", weather.Current.WindSpeed)
	result += fmt.Sprintf("湿度：%d%%\n", weather.Current.RelativeHumidity)

	return result, nil
}

type weatherResponse struct {
	Current struct {
		Temperature         float64 `json:"temperature_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		WeatherCode         int     `json:"weather_code"`
		WindSpeed           float64 `json:"wind_speed_10m"`
		RelativeHumidity    int     `json:"relative_humidity_2m"`
	} `json:"current"`
}

type geocodeResponse struct {
	Results []struct {
		Name      string  `json:"name"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Country   string  `json:"country"`
	} `json:"results"`
}

func geocodeCity(ctx context.Context, city string) (float64, float64, error) {
	apiURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=zh", url.QueryEscape(city))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return 0, 0, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	// 检查响应是否是 JSON
	if len(body) > 0 && body[0] != '{' && body[0] != '[' {
		return 0, 0, fmt.Errorf("API 返回非 JSON 响应（可能被限流或封锁）")
	}

	var result geocodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(result.Results) == 0 {
		return 0, 0, fmt.Errorf("找不到城市：%s", city)
	}

	return result.Results[0].Latitude, result.Results[0].Longitude, nil
}

func getWeather(ctx context.Context, lat, lon float64) (*weatherResponse, error) {
	apiURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,apparent_temperature,weather_code,wind_speed_10m,relative_humidity_2m", lat, lon)

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

	var result weatherResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func weatherCodeToDescription(code int) string {
	codes := map[int]string{
		0:  "晴朗",
		1:  "主要晴朗",
		2:  "局部多云",
		3:  "多云",
		45: "雾",
		48: "雾凇",
		51: "小毛毛雨",
		53: "中毛毛雨",
		55: "大毛毛雨",
		61: "小雨",
		63: "中雨",
		65: "大雨",
		71: "小雪",
		73: "中雪",
		75: "大雪",
		80: "小阵雨",
		81: "中阵雨",
		82: "大阵雨",
		95: "雷暴",
		96: "雷暴伴小冰雹",
		99: "雷暴伴大冰雹",
	}

	if desc, ok := codes[code]; ok {
		return desc
	}
	return "未知"
}
