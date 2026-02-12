//go:build go1.18

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// FuzzRegisterRequest fuzzes user registration input parsing
// This tests that the handler doesn't panic on malformed input
func FuzzRegisterRequest(f *testing.F) {
	// Add seed corpus
	f.Add(`{"email":"test@example.com","password":"password123"}`)
	f.Add(`{"email":"","password":""}`)
	f.Add(`{"email":"invalid-email","password":"short"}`)
	f.Add(`not json at all`)
	f.Add(`{"email":"test@example.com","password":"password123","telegram_chat_id":"123456"}`)
	f.Add(`{"email":null,"password":null}`)
	f.Add(`{}`)
	f.Add(`[]`)
	f.Add(`null`)
	f.Add(`{"email":"a@b.c","password":"12345678"}`)                                                                                                                           // Valid but minimal
	f.Add(`{"email":"verylongemailaddressverylongemailaddressverylongemailaddressverylongemailaddress@verylongdomainverylongdomainverylongdomain.com","password":"12345678"}`) // Very long email

	f.Fuzz(func(t *testing.T, input string) {
		gin.SetMode(gin.TestMode)
		handler := NewUserHandler(nil, nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBufferString(input))
		c.Request.Header.Set("Content-Type", "application/json")

		// Handler should not panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Handler panicked with input: %q, panic: %v", input, r)
				}
			}()
			handler.RegisterUser(c)
		}()

		// Response should be valid HTTP status code
		if w.Code < 100 || w.Code > 599 {
			t.Errorf("Invalid HTTP status code %d for input: %q", w.Code, input)
		}
	})
}

// FuzzLoginRequest fuzzes user login input parsing
func FuzzLoginRequest(f *testing.F) {
	f.Add(`{"email":"test@example.com","password":"password123"}`)
	f.Add(`{"email":"","password":""}`)
	f.Add(`{}`)
	f.Add(`[]`)
	f.Add(`null`)
	f.Add(`{"email":"x","password":"y"}`)
	f.Add(`{"email":123,"password":456}`) // Wrong types

	f.Fuzz(func(t *testing.T, input string) {
		gin.SetMode(gin.TestMode)
		handler := NewUserHandler(nil, nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/users/login", bytes.NewBufferString(input))
		c.Request.Header.Set("Content-Type", "application/json")

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Handler panicked with input: %q, panic: %v", input, r)
			}
		}()

		handler.LoginUser(c)

		// Response should be valid HTTP status code
		if w.Code < 100 || w.Code > 599 {
			t.Errorf("Invalid HTTP status code %d for input: %q", w.Code, input)
		}
	})
}

// FuzzJSONParsing fuzzes general JSON parsing behavior
// This tests that malformed JSON is handled gracefully
func FuzzJSONParsing(f *testing.F) {
	f.Add(`{"key": "value"}`)
	f.Add(`{"key": 123}`)
	f.Add(`{"key": null}`)
	f.Add(`{"key": [1,2,3]}`)
	f.Add(`{"key": {"nested": true}}`)
	f.Add(`{"threshold": 0.5}`)
	f.Add(`{"profit_threshold": 1.5, "symbols": ["BTC/USDT"]}`)
	f.Add(`{}`)
	f.Add(`{"threshold": "not a number"}`)
	f.Add(`{"threshold": 999999999999999999999999999999}`) // Overflow
	f.Add(`{"a":1,"a":2}`)                                 // Duplicate keys
	f.Add(`{"unicode": "日本語"}`)                            // Unicode
	f.Add(`{"escape": "\n\t\r"}`)                          // Escape sequences

	f.Fuzz(func(t *testing.T, input string) {
		// Just verify JSON parsing doesn't panic
		var result map[string]interface{}
		_ = json.Unmarshal([]byte(input), &result)

		var resultArray []interface{}
		_ = json.Unmarshal([]byte(input), &resultArray)

		var resultString string
		_ = json.Unmarshal([]byte(input), &resultString)
	})
}

// FuzzAlertConditions fuzzes alert conditions structure parsing
func FuzzAlertConditions(f *testing.F) {
	f.Add(`{"threshold": 0.5}`)
	f.Add(`{"profit_threshold": 1.5, "symbols": ["BTC/USDT"]}`)
	f.Add(`{"price_threshold": 50000, "volume_threshold": 1000000}`)
	f.Add(`{}`)
	f.Add(`{"threshold": "not a number"}`)
	f.Add(`{"symbols": null}`)
	f.Add(`{"symbols": []}`)
	f.Add(`{"symbols": ["a", "b", "c"]}`)

	f.Fuzz(func(t *testing.T, input string) {
		// Parse as generic map
		var conditions map[string]interface{}
		err := json.Unmarshal([]byte(input), &conditions)

		if err == nil {
			// If parsing succeeded, verify we can access fields safely
			if threshold, ok := conditions["threshold"]; ok {
				// Try to convert to float64 (shouldn't panic)
				switch v := threshold.(type) {
				case float64:
					_ = v
				case string:
					_ = v
				case nil:
					// nil is valid
				}
			}

			if symbols, ok := conditions["symbols"]; ok {
				// Try to convert to slice
				if arr, ok := symbols.([]interface{}); ok {
					for _, s := range arr {
						if str, ok := s.(string); ok {
							_ = str
						}
					}
				}
			}
		}
	})
}

// FuzzContentTypeHandling fuzzes different content types
func FuzzContentTypeHandling(f *testing.F) {
	f.Add("application/json", `{"key":"value"}`)
	f.Add("text/plain", "plain text")
	f.Add("application/x-www-form-urlencoded", "key=value")
	f.Add("", `{"key":"value"}`)
	f.Add("application/json; charset=utf-8", `{"key":"value"}`)
	f.Add("multipart/form-data", "boundary data")

	f.Fuzz(func(t *testing.T, contentType, body string) {
		gin.SetMode(gin.TestMode)
		handler := NewUserHandler(nil, nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBufferString(body))
		if contentType != "" {
			c.Request.Header.Set("Content-Type", contentType)
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Handler panicked with contentType=%q, body=%q, panic: %v",
					contentType, body, r)
			}
		}()

		handler.RegisterUser(c)
	})
}

// FuzzQueryParameters fuzzes query parameter parsing
func FuzzQueryParameters(f *testing.F) {
	f.Add("limit=10&offset=0")
	f.Add("limit=-1")
	f.Add("limit=999999999999")
	f.Add("invalid")
	f.Add("a=1&a=2&a=3")     // Repeated keys
	f.Add("key=value&key2=") // Empty value
	f.Add("=value")          // Empty key
	f.Add("")                // Empty query

	f.Fuzz(func(t *testing.T, query string) {
		gin.SetMode(gin.TestMode)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test?"+query, nil)

		// Access query parameters - shouldn't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Query parsing panicked with query=%q, panic: %v", query, r)
			}
		}()

		_ = c.Query("limit")
		_ = c.Query("offset")
		_ = c.QueryArray("a")
		_ = c.DefaultQuery("missing", "default")
	})
}

// FuzzPathParameters fuzzes path parameter handling
func FuzzPathParameters(f *testing.F) {
	f.Add("123")
	f.Add("")
	f.Add("abc-def-ghi")
	f.Add("user@domain.com")
	f.Add("with/slash")
	f.Add("with%20encoded")
	f.Add("../../../etc/passwd")                                                                                                   // Path traversal attempt
	f.Add("verylongparamverylongparamverylongparamverylongparamverylongparamverylongparamverylongparamverylongparamverylongparam") // Long param

	f.Fuzz(func(t *testing.T, param string) {
		// Skip params with control characters (including null bytes)
		// as they cause httptest.NewRequest to panic - this is expected behavior
		for _, c := range param {
			if c < 32 || c == 127 {
				return // Skip control characters
			}
		}

		gin.SetMode(gin.TestMode)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/test/"+param, nil)
		c.Params = gin.Params{{Key: "id", Value: param}}

		// Access path parameters - shouldn't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Path param access panicked with param=%q, panic: %v", param, r)
			}
		}()

		_ = c.Param("id")
		_ = c.Param("missing")
	})
}
