// Package tools 提供工具接口和 Mock 工具实现
package tools

import "context"

// Tool 工具接口
type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, args string) (string, error)
}

// Spec 给测试用例生成器用的工具说明书
type Spec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Registry 工具注册表
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

func (r *Registry) Specs() []Spec {
	specs := make([]Spec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, Spec{Name: t.Name(), Description: t.Description()})
	}
	return specs
}
