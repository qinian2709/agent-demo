// Package trace 提供生产级 OpenTelemetry trace 封装
// 支持：
// 1. 按环境变量开关 (ENABLE_TRACING=true) 控制指定环境上报、其他环境零开销关闭
// 2. 支持 HTTP / HTTPS 的 OTLP Endpoint（如本地 localhost:6006 或远端 https://agent-phoenix.coolify.eeo-inc.com）
// 3. 支持复用上游 X-Request-ID 作为全链路唯一 Trace ID
package trace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("agent")

// IsTracingEnabled 判断当前环境是否开启 Trace 上报
// 只有显式配置了 ENABLE_TRACING=true 的环境才会上报，其他环境默认关闭（零开销）
func IsTracingEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_TRACING")), "true")
}

// InitTracer 初始化 tracer（应用启动时调用一次）
func InitTracer(serviceName string, opts ...TracerOption) (*sdktrace.TracerProvider, error) {
	// 设置全局 propagator（即使不上报，也支持从 HTTP header 解析/传递 trace_id）
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, // W3C Trace Context
		propagation.Baggage{},
	))

	if !IsTracingEnabled() {
		fmt.Println("[trace] tracing disabled (set ENABLE_TRACING=true to enable)")
		return nil, nil
	}

	rawEndpoint := strings.TrimSpace(os.Getenv("PHOENIX_ENDPOINT"))
	if rawEndpoint == "" {
		fmt.Println("[trace] PHOENIX_ENDPOINT is empty, tracing disabled")
		return nil, nil
	}

	var clientOpts []otlptracehttp.Option
	if strings.HasPrefix(rawEndpoint, "http://") || strings.HasPrefix(rawEndpoint, "https://") {
		u, err := url.Parse(rawEndpoint)
		if err != nil {
			return nil, fmt.Errorf("invalid PHOENIX_ENDPOINT %q: %w", rawEndpoint, err)
		}
		clientOpts = append(clientOpts, otlptracehttp.WithEndpoint(u.Host))
		if u.Scheme == "http" {
			clientOpts = append(clientOpts, otlptracehttp.WithInsecure())
		}
		if u.Path != "" && u.Path != "/" {
			clientOpts = append(clientOpts, otlptracehttp.WithURLPath(u.Path))
		}
	} else {
		clientOpts = append(clientOpts,
			otlptracehttp.WithEndpoint(rawEndpoint),
			otlptracehttp.WithInsecure(),
		)
	}

	ctx := context.Background()
	exporter, err := otlptracehttp.New(ctx, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	// 应用选项
	projectName := strings.TrimSpace(os.Getenv("PHOENIX_PROJECT_NAME"))
	if projectName == "" {
		projectName = serviceName
	}
	cfg := &tracerConfig{
		serviceName: serviceName,
		projectName: projectName,
		environment: os.Getenv("APP_ENV"),
	}
	if cfg.environment == "" {
		cfg.environment = "default"
	}
	for _, opt := range opts {
		opt(cfg)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.serviceName),
			// Arize Phoenix 根据 openinference.project.name 自动创建并归类 Projects 看板
			attribute.String("openinference.project.name", cfg.projectName),
			attribute.String("environment", cfg.environment),
		)),
	)
	otel.SetTracerProvider(tp)

	fmt.Printf("[trace] OTLP tracing enabled -> endpoint: %s, project: %s, service: %s\n",
		rawEndpoint, cfg.projectName, serviceName)
	return tp, nil
}

// TracerOption 配置选项
type TracerOption func(*tracerConfig)

type tracerConfig struct {
	serviceName string
	projectName string
	environment string
}

func WithProjectName(projectName string) TracerOption {
	return func(c *tracerConfig) {
		if strings.TrimSpace(projectName) != "" {
			c.projectName = strings.TrimSpace(projectName)
		}
	}
}

func WithEnvironment(env string) TracerOption {
	return func(c *tracerConfig) {
		c.environment = env
	}
}

// SpanOption span 配置选项
type SpanOption func(*spanConfig)

type spanConfig struct {
	attributes []attribute.KeyValue
	spanKind   trace.SpanKind
}

func WithAttributes(attrs ...attribute.KeyValue) SpanOption {
	return func(c *spanConfig) {
		c.attributes = append(c.attributes, attrs...)
	}
}

func WithSpanKind(kind trace.SpanKind) SpanOption {
	return func(c *spanConfig) {
		c.spanKind = kind
	}
}

// Trace 通用 trace 函数，支持任意输入输出类型
func Trace[T any, R any](
	ctx context.Context,
	spanName string,
	input T,
	fn func(ctx context.Context, input T) (R, error),
	opts ...SpanOption,
) (R, error) {
	cfg := &spanConfig{
		spanKind: trace.SpanKindInternal,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(cfg.spanKind))
	defer span.End()

	// 记录输入（同时兼容 OpenInference / Arize Phoenix 标准字段与通用字段）
	inputStr := formatValue(input)
	span.SetAttributes(
		attribute.String("input.value", inputStr),
	)
	for _, attr := range cfg.attributes {
		span.SetAttributes(attr)
	}

	start := time.Now()
	result, err := fn(ctx, input)
	elapsed := time.Since(start)

	// 记录输出
	outputStr := formatValue(result)
	span.SetAttributes(
		attribute.String("output.value", outputStr),
		attribute.Int64("latency_ms", elapsed.Milliseconds()),
		attribute.String("latency_human", elapsed.String()),
	)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return result, err
}

// TraceWithTimeout 带超时的通用 trace 函数
func TraceWithTimeout[T any, R any](
	ctx context.Context,
	spanName string,
	input T,
	timeout time.Duration,
	fn func(ctx context.Context, input T) (R, error),
	opts ...SpanOption,
) (R, error) {
	cfg := &spanConfig{
		spanKind: trace.SpanKindClient,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	spanName = fmt.Sprintf("%s(timeout=%s)", spanName, timeout)
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(cfg.spanKind))
	defer span.End()

	span.SetAttributes(
		attribute.String("input.value", formatValue(input)),
		attribute.String("timeout", timeout.String()),
	)
	for _, attr := range cfg.attributes {
		span.SetAttributes(attr)
	}

	start := time.Now()

	type result struct {
		data R
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		data, err := fn(ctx, input)
		ch <- result{data, err}
	}()

	select {
	case res := <-ch:
		elapsed := time.Since(start)
		span.SetAttributes(
			attribute.String("output.value", formatValue(res.data)),
			attribute.Int64("latency_ms", elapsed.Milliseconds()),
			attribute.String("latency_human", elapsed.String()),
			attribute.Bool("timeout_hit", false),
		)
		if res.err != nil {
			span.SetStatus(codes.Error, res.err.Error())
			span.RecordError(res.err)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return res.data, res.err

	case <-time.After(timeout):
		elapsed := time.Since(start)
		err := fmt.Errorf("operation timeout after %s", timeout)
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		span.SetAttributes(
			attribute.Int64("latency_ms", elapsed.Milliseconds()),
			attribute.String("latency_human", elapsed.String()+" (TIMEOUT)"),
			attribute.Bool("timeout_hit", true),
		)
		var zero R
		return zero, err
	}
}

// TraceStream 流式调用的 trace 函数（适用于 SSE/WebSocket）
func TraceStream[T any](
	ctx context.Context,
	spanName string,
	input T,
	fn func(ctx context.Context, input T, onChunk func(chunk string) error) error,
	opts ...SpanOption,
) error {
	cfg := &spanConfig{
		spanKind: trace.SpanKindClient,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(cfg.spanKind))
	defer span.End()

	span.SetAttributes(
		attribute.String("input.value", formatValue(input)),
		attribute.String("stream", "true"),
	)
	for _, attr := range cfg.attributes {
		span.SetAttributes(attr)
	}

	start := time.Now()
	var chunks []string

	err := fn(ctx, input, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})

	elapsed := time.Since(start)
	span.SetAttributes(
		attribute.Int("stream.chunks", len(chunks)),
		attribute.Int64("latency_ms", elapsed.Milliseconds()),
		attribute.String("latency_human", elapsed.String()),
	)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return err
}

// 便捷函数（遵循 Arize Phoenix / OpenInference span kind 规范：AGENT / LLM / TOOL）

// TraceAgent 追踪 Agent 任务
func TraceAgent[R any](
	ctx context.Context,
	agentName, input string,
	fn func(ctx context.Context) (R, error),
) (R, error) {
	spanName := fmt.Sprintf("%s: %s", agentName, truncate(input, 30))
	return Trace(ctx, spanName, input, func(ctx context.Context, _ string) (R, error) {
		return fn(ctx)
	}, WithAttributes(
		attribute.String("openinference.span.kind", "AGENT"),
		attribute.String("agent_name", agentName),
	))
}

// TraceLLM 追踪 LLM 调用
func TraceLLM[R any](
	ctx context.Context,
	model, prompt string,
	fn func(ctx context.Context) (R, int, error),
) (R, error) {
	spanName := fmt.Sprintf("llm_call: %s", truncate(model, 20))
	return Trace(ctx, spanName, prompt, func(ctx context.Context, _ string) (R, error) {
		result, tokens, err := fn(ctx)
		if tokens > 0 {
			trace.SpanFromContext(ctx).SetAttributes(attribute.Int("llm.token_count.total", tokens))
		}
		return result, err
	}, WithAttributes(
		attribute.String("openinference.span.kind", "LLM"),
		attribute.String("llm.model_name", model),
		attribute.String("llm.model", model),
	))
}

// TraceTool 追踪工具调用
func TraceTool[R any](
	ctx context.Context,
	toolName string,
	args interface{},
	fn func(ctx context.Context) (R, error),
) (R, error) {
	spanName := fmt.Sprintf("tool_call: %s", toolName)
	return Trace(ctx, spanName, args, func(ctx context.Context, _ interface{}) (R, error) {
		return fn(ctx)
	}, WithAttributes(
		attribute.String("openinference.span.kind", "TOOL"),
		attribute.String("tool.name", toolName),
	))
}

// TraceToolWithTimeout 带超时的工具调用
func TraceToolWithTimeout[R any](
	ctx context.Context,
	toolName string,
	args interface{},
	timeout time.Duration,
	fn func(ctx context.Context) (R, error),
) (R, error) {
	spanName := fmt.Sprintf("tool_call: %s", toolName)
	return TraceWithTimeout(ctx, spanName, args, timeout, func(ctx context.Context, _ interface{}) (R, error) {
		return fn(ctx)
	}, WithAttributes(
		attribute.String("openinference.span.kind", "TOOL"),
		attribute.String("tool.name", toolName),
	))
}

// 工具函数

func formatValue(v interface{}) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	return truncate(s, 500)
}

func truncate(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "..."
	}
	return s
}

// ExtractTraceContext 从 HTTP header 提取已有的 trace context
func ExtractTraceContext(ctx context.Context, headers map[string]string) context.Context {
	propagator := otel.GetTextMapPropagator()
	return propagator.Extract(ctx, propagation.MapCarrier(headers))
}

// ExtractTraceContextFromHTTP 从 http.Header 提取 trace context
func ExtractTraceContextFromHTTP(ctx context.Context, headers map[string][]string) context.Context {
	propagator := otel.GetTextMapPropagator()
	return propagator.Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectTraceContext 将当前 trace context 注入到 map 中
func InjectTraceContext(ctx context.Context) map[string]string {
	headers := make(map[string]string)
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.MapCarrier(headers))
	return headers
}

// GetTraceID 获取当前 context 中的 trace ID
func GetTraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasTraceID() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// GetSpanID 获取当前 context 中的 span ID
func GetSpanID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasSpanID() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}

// ContextFromRequestID 从 X-Request-ID 创建 trace context
// 将业务 request ID 转换为 OpenTelemetry trace ID，实现单 ID 全链路追踪
func ContextFromRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(requestID)))
	traceIDHex := hash[:32]

	traceID, err := trace.TraceIDFromHex(traceIDHex)
	if err != nil {
		return ctx
	}

	// SpanID 也必须非空，否则 SpanContext.IsValid() 为 false，OpenTelemetry 不会复用该 TraceID
	var spanID trace.SpanID
	if sid, err := trace.SpanIDFromHex(hash[32:48]); err == nil {
		spanID = sid
	}

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	return trace.ContextWithRemoteSpanContext(ctx, spanContext)
}

// ContextFromRequest 从 HTTP 请求创建 trace context
// 自动从 X-Request-ID header 提取并转换为 trace context
func ContextFromRequest(ctx context.Context, r *http.Request) context.Context {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return ContextFromRequestID(ctx, requestID)
}
