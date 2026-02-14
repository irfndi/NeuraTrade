package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

type mockAIRegistry struct {
	models      []ai.ModelInfo
	providers   []ai.ProviderInfo
	err         error
	findErr     error
	providerErr error
}

func (m *mockAIRegistry) GetRegistry(ctx context.Context) (*ai.ModelRegistry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ai.ModelRegistry{
		Models:    m.models,
		Providers: m.providers,
	}, nil
}

func (m *mockAIRegistry) FindModel(ctx context.Context, modelID string) (*ai.ModelInfo, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, model := range m.models {
		if model.ModelID == modelID {
			return &model, nil
		}
	}
	return nil, nil
}

func (m *mockAIRegistry) GetModelsByProvider(ctx context.Context, providerID string) ([]ai.ModelInfo, error) {
	var result []ai.ModelInfo
	for _, model := range m.models {
		if model.ProviderID == providerID {
			result = append(result, model)
		}
	}
	return result, nil
}

func (m *mockAIRegistry) GetActiveProviders(ctx context.Context) ([]ai.ProviderInfo, error) {
	if m.providerErr != nil {
		return nil, m.providerErr
	}
	return m.providers, nil
}

func setupAIHandlerTest() (*AIHandler, *mockAIRegistry) {
	mock := &mockAIRegistry{
		models: []ai.ModelInfo{
			{
				ModelID:     "gpt-4",
				ProviderID:  "openai",
				DisplayName: "GPT-4",
				Status:      "active",
				Capabilities: ai.ModelCapability{
					SupportsTools:  true,
					SupportsVision: false,
				},
				Cost: ai.ModelCost{
					InputCost:  decimal.NewFromFloat(30.0),
					OutputCost: decimal.NewFromFloat(60.0),
				},
				LatencyClass: "medium",
				Limits: ai.ModelLimits{
					ContextLimit: 8192,
				},
			},
			{
				ModelID:     "gpt-3.5-turbo",
				ProviderID:  "openai",
				DisplayName: "GPT-3.5 Turbo",
				Status:      "active",
				Capabilities: ai.ModelCapability{
					SupportsTools:  true,
					SupportsVision: false,
				},
				Cost: ai.ModelCost{
					InputCost:  decimal.NewFromFloat(0.5),
					OutputCost: decimal.NewFromFloat(1.5),
				},
				LatencyClass: "fast",
				Limits: ai.ModelLimits{
					ContextLimit: 4096,
				},
			},
			{
				ModelID:     "claude-3",
				ProviderID:  "anthropic",
				DisplayName: "Claude 3",
				Status:      "inactive",
				Capabilities: ai.ModelCapability{
					SupportsTools:  true,
					SupportsVision: true,
				},
				Cost: ai.ModelCost{
					InputCost:  decimal.NewFromFloat(15.0),
					OutputCost: decimal.NewFromFloat(75.0),
				},
				LatencyClass: "medium",
				Limits: ai.ModelLimits{
					ContextLimit: 200000,
				},
			},
		},
		providers: []ai.ProviderInfo{
			{ID: "openai", Name: "OpenAI"},
			{ID: "anthropic", Name: "Anthropic"},
		},
	}
	handler := NewAIHandler(mock)
	return handler, mock
}

func TestNewAIHandler(t *testing.T) {
	mock := &mockAIRegistry{}
	handler := NewAIHandler(mock)
	assert.NotNil(t, handler)
	assert.NotNil(t, handler.registry)
}

func TestAIHandler_GetModels_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := setupAIHandlerTest()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/ai/models", nil)

	handler.GetModels(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "gpt-4")
	assert.Contains(t, w.Body.String(), "gpt-3.5-turbo")
}

func TestAIHandler_GetModels_WithProviderFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := setupAIHandlerTest()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/ai/models?provider=openai", nil)

	handler.GetModels(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "openai")
	assert.NotContains(t, w.Body.String(), "anthropic")
}

func TestAIHandler_GetModels_RegistryError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mock := &mockAIRegistry{err: assert.AnError}
	handler := NewAIHandler(mock)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/api/ai/models", nil)

	handler.GetModels(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to get models")
}

func TestAIHandler_RouteModel_NoBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := setupAIHandlerTest()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/ai/route", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RouteModel(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAIHandler_RouteModel_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := setupAIHandlerTest()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/api/ai/route", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RouteModel(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAIModelInfo_JSONSerialization(t *testing.T) {
	info := AIModelInfo{
		ModelID:        "test-model",
		Provider:       "test-provider",
		DisplayName:    "Test Model",
		SupportsTools:  true,
		SupportsVision: false,
		Cost:           "10.00",
		LatencyClass:   "fast",
		ContextLimit:   4096,
	}

	data, err := json.Marshal(info)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "test-model")
	assert.Contains(t, string(data), "test-provider")
}
