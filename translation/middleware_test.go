package translation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPMiddleware(t *testing.T) {
	translator := NewMockTranslator()

	tests := []struct {
		name         string
		headerName   string
		headerValue  string
		expectedLang string
	}{
		{
			name:         "X-Language header with Russian",
			headerName:   "X-Language",
			headerValue:  "ru",
			expectedLang: "ru",
		},
		{
			name:         "X-Language header with Kazakh",
			headerName:   "X-Language",
			headerValue:  "kk",
			expectedLang: "kk",
		},
		{
			name:         "Accept-Language header",
			headerName:   "Accept-Language",
			headerValue:  "kk",
			expectedLang: "kk",
		},
		{
			name:         "Invalid language defaults to Russian",
			headerName:   "X-Language",
			headerValue:  "en",
			expectedLang: "ru",
		},
		{
			name:         "No language header defaults to Russian",
			headerName:   "",
			headerValue:  "",
			expectedLang: "ru",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler that checks the context
			var contextLang Language
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				contextLang = LanguageFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with middleware
			middleware := translator.HTTPMiddleware(handler)

			// Create request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.headerName != "" {
				req.Header.Set(tt.headerName, tt.headerValue)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Execute
			middleware.ServeHTTP(rr, req)

			// Check language in context
			if contextLang.Code != tt.expectedLang {
				t.Errorf("Language in context = %s, want %s", contextLang.Code, tt.expectedLang)
			}
		})
	}
}

func TestLanguageFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "context with Russian language",
			ctx:      context.WithValue(context.Background(), languageKey, LanguageRu),
			expected: "ru",
		},
		{
			name:     "context with Kazakh language",
			ctx:      context.WithValue(context.Background(), languageKey, LanguageKk),
			expected: "kk",
		},
		{
			name:     "context without language yields the zero Language",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with wrong type yields the zero Language",
			ctx:      context.WithValue(context.Background(), languageKey, "invalid"),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang := LanguageFromContext(tt.ctx)
			if lang.Code != tt.expected {
				t.Errorf("LanguageFromContext() = %s, want %s", lang.Code, tt.expected)
			}
		})
	}
}

func TestSetLanguageInContext(t *testing.T) {
	ctx := context.Background()

	// Set language
	ctx = SetLanguageInContext(ctx, LanguageKk)

	// Retrieve language
	lang := LanguageFromContext(ctx)
	if lang.Code != "kk" {
		t.Errorf("Language = %s, want kk", lang.Code)
	}
}

func TestFindLanguage(t *testing.T) {
	translator := NewMockTranslator()

	tests := []struct {
		name     string
		code     string
		wantCode string
		wantErr  bool
	}{
		{
			name:     "find Russian",
			code:     "ru",
			wantCode: "ru",
			wantErr:  false,
		},
		{
			name:     "find Kazakh",
			code:     "kk",
			wantCode: "kk",
			wantErr:  false,
		},
		{
			name:     "invalid language returns default",
			code:     "en",
			wantCode: "ru",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, err := translator.findLanguage(tt.code)

			if (err != nil) != tt.wantErr {
				t.Errorf("findLanguage() error = %v, wantErr %v", err, tt.wantErr)
			}

			if lang.Code != tt.wantCode {
				t.Errorf("findLanguage() code = %s, want %s", lang.Code, tt.wantCode)
			}
		})
	}
}

func TestHTTPMiddleware_XLanguagePriority(t *testing.T) {
	translator := NewMockTranslator()

	// Create a test handler
	var contextLang Language
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextLang = LanguageFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := translator.HTTPMiddleware(handler)

	// Create request with both headers (X-Language should take priority)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Language", "kk")
	req.Header.Set("Accept-Language", "ru")

	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)

	// Should use X-Language (kk) not Accept-Language (ru)
	if contextLang.Code != "kk" {
		t.Errorf("Expected X-Language to take priority, got %s", contextLang.Code)
	}
}
