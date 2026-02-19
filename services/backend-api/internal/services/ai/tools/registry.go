package tools

import (
	"encoding/json"

	"github.com/irfndi/neuratrade/internal/ai/llm"
)

type Tool interface {
	Name() string
	Description() string
	Execute(params map[string]interface{}) (interface{}, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) GetAllTools() []Tool {
	var tools []Tool
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

func (r *Registry) GetToolsForStrategy(strategy string) []llm.ToolDefinition {
	var defs []llm.ToolDefinition
	for _, tool := range r.tools {
		if td, ok := tool.(ToolWithDefinition); ok {
			defs = append(defs, td.GetDefinition())
		}
	}
	return defs
}

func (r *Registry) ExecuteTool(name string, params json.RawMessage) (json.RawMessage, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, ErrToolNotFound
	}

	var p map[string]interface{}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, ErrInvalidParams
		}
	}

	result, err := tool.Execute(p)
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

type ToolWithDefinition interface {
	Tool
	GetDefinition() llm.ToolDefinition
}

var (
	ErrToolNotFound  = &ToolError{Code: "tool_not_found", Message: "tool not found"}
	ErrInvalidParams = &ToolError{Code: "invalid_params", Message: "invalid parameters"}
)

type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string {
	return e.Message
}
