package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	assert.NotNil(t, r)
	assert.NotNil(t, r.tools)
	assert.Empty(t, r.tools)
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	tool := &mockTool{name: "test_tool"}
	r.Register(tool)

	assert.Len(t, r.tools, 1)
	assert.Equal(t, tool, r.tools["test_tool"])
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test_tool"}
	r.Register(tool)

	// Get existing tool
	got, ok := r.Get("test_tool")
	assert.True(t, ok)
	assert.Equal(t, tool, got)

	// Get non-existing tool
	got, ok = r.Get("non_existing")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestRegistry_GetAllTools(t *testing.T) {
	r := NewRegistry()

	tool1 := &mockTool{name: "tool1"}
	tool2 := &mockTool{name: "tool2"}
	r.Register(tool1)
	r.Register(tool2)

	tools := r.GetAllTools()
	assert.Len(t, tools, 2)
}

func TestRegistry_ExecuteTool(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{
		name: "test_tool",
		execute: func(params map[string]interface{}) (interface{}, error) {
			return "success", nil
		},
	}
	r.Register(tool)

	t.Run("execute existing tool", func(t *testing.T) {
		params := json.RawMessage(`{"key": "value"}`)
		result, err := r.ExecuteTool("test_tool", params)
		require.NoError(t, err)
		assert.Equal(t, json.RawMessage(`"success"`), result)
	})

	t.Run("execute non-existing tool", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		result, err := r.ExecuteTool("non_existing", params)
		assert.Error(t, err)
		assert.Equal(t, ErrToolNotFound, err)
		assert.Nil(t, result)
	})

	t.Run("execute with invalid params", func(t *testing.T) {
		params := json.RawMessage(`{invalid json}`)
		result, err := r.ExecuteTool("test_tool", params)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidParams, err)
		assert.Nil(t, result)
	})

	t.Run("execute with empty params", func(t *testing.T) {
		result, err := r.ExecuteTool("test_tool", nil)
		require.NoError(t, err)
		assert.Equal(t, json.RawMessage(`"success"`), result)
	})
}

func TestToolError(t *testing.T) {
	err := &ToolError{Code: "test_code", Message: "test message"}
	assert.Equal(t, "test message", err.Error())
}

// Mock tool for testing
type mockTool struct {
	name        string
	description string
	execute     func(params map[string]interface{}) (interface{}, error)
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) Execute(params map[string]interface{}) (interface{}, error) {
	if m.execute != nil {
		return m.execute(params)
	}
	return nil, nil
}
