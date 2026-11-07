package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/httpx"
	"kumparan-assessment/internal/pagination"
)

type errorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Fields    []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	} `json:"error"`
}

type createRequest struct {
	Title    string `json:"title" binding:"required,max=10"`
	AuthorID string `json:"author_id" binding:"required"`
}

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(httpx.RequestID(), httpx.Recovery())
	r.NoRoute(httpx.NotFound)
	r.NoMethod(httpx.MethodNotAllowed)
	return r
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func serve(r *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var resp errorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body %q is not an error envelope", w.Body.String())
	return resp
}

func TestListNeverReturnsNull(t *testing.T) {
	r := newRouter()
	r.GET("/empty", func(c *gin.Context) { httpx.List[string](c, nil, "") })
	r.GET("/more", func(c *gin.Context) { httpx.List(c, []string{"a"}, "abc") })

	assert.JSONEq(t, `{"data":[],"next_cursor":null}`, serve(r, http.MethodGet, "/empty", "").Body.String())
	assert.JSONEq(t, `{"data":["a"],"next_cursor":"abc"}`, serve(r, http.MethodGet, "/more", "").Body.String())
}

func TestErrorMapping(t *testing.T) {
	captureLogs(t)

	r := newRouter()
	r.GET("/author", func(c *gin.Context) {
		httpx.Error(c, fmt.Errorf("insert article: %w", apperr.ErrAuthorNotFound))
	})
	r.GET("/article", func(c *gin.Context) {
		httpx.Error(c, fmt.Errorf("update article: %w", apperr.ErrArticleNotFound))
	})
	r.GET("/no-fields", func(c *gin.Context) {
		httpx.Error(c, apperr.ErrNoFieldsToUpdate)
	})
	r.GET("/too-broad", func(c *gin.Context) {
		httpx.Error(c, apperr.ErrAuthorTooBroad)
	})
	r.GET("/list", func(c *gin.Context) {
		if _, err := pagination.ParseLimit(c.Query("limit")); err != nil {
			httpx.Error(c, err)
			return
		}
		if c.Query("cursor") != "" {
			if _, err := pagination.Decode(c.Query("cursor")); err != nil {
				httpx.Error(c, err)
				return
			}
		}
		httpx.List[string](c, nil, "")
	})
	r.GET("/timeout", func(c *gin.Context) {
		httpx.Error(c, fmt.Errorf("query: %w", context.DeadlineExceeded))
	})

	cases := []struct {
		target string
		status int
		code   string
	}{
		{"/author", http.StatusUnprocessableEntity, "author_not_found"},
		{"/article", http.StatusNotFound, "article_not_found"},
		{"/no-fields", http.StatusBadRequest, "no_fields_to_update"},
		{"/too-broad", http.StatusBadRequest, "author_too_broad"},
		{"/list?cursor=lol", http.StatusBadRequest, "invalid_cursor"},
		{"/list?limit=abc", http.StatusBadRequest, "invalid_limit"},
		{"/list?limit=1000", http.StatusBadRequest, "invalid_limit"},
		{"/timeout", http.StatusServiceUnavailable, "timeout"},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			w := serve(r, http.MethodGet, tc.target, "")
			resp := decodeError(t, w)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, resp.Error.Code)
			assert.NotEmpty(t, resp.Error.RequestID)
		})
	}
}

func TestInternalErrorIsNotLeaked(t *testing.T) {
	logs := captureLogs(t)
	secret := `failed to connect to postgres://user:hunter2@db:5432/articles: relation "articles" does not exist`

	r := newRouter()
	r.GET("/boom", func(c *gin.Context) { httpx.Error(c, fmt.Errorf("list articles: %w", errors.New(secret))) })

	w := serve(r, http.MethodGet, "/boom", "")
	resp := decodeError(t, w)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "internal_error", resp.Error.Code)
	assert.NotContains(t, w.Body.String(), "hunter2")
	assert.NotContains(t, w.Body.String(), "relation")

	require.NotEmpty(t, resp.Error.RequestID)
	assert.Contains(t, logs.String(), "hunter2", "server log should keep the full error")
	assert.Contains(t, logs.String(), resp.Error.RequestID, "server log should carry the request_id")
	assert.Equal(t, resp.Error.RequestID, w.Header().Get(httpx.RequestIDHeader))
}

func TestClientCancelIsNotAServerError(t *testing.T) {
	logs := captureLogs(t)

	r := newRouter()
	r.GET("/slow", func(c *gin.Context) { httpx.Error(c, fmt.Errorf("query: %w", context.Canceled)) })

	w := serve(r, http.MethodGet, "/slow", "")
	assert.Equal(t, 499, w.Code)
	assert.Empty(t, w.Body.String())
	assert.NotContains(t, logs.String(), "level=ERROR")
}

func TestBindJSON(t *testing.T) {
	r := newRouter()
	r.POST("/articles", func(c *gin.Context) {
		var req createRequest
		if !httpx.BindJSON(c, &req) {
			return
		}
		httpx.Created(c, req)
	})

	cases := []struct {
		name   string
		body   string
		code   string
		fields map[string]string
	}{
		{"missing fields", `{}`, "validation_failed", map[string]string{"title": "is required", "author_id": "is required"}},
		{"too long", `{"title":"12345678901","author_id":"x"}`, "validation_failed", map[string]string{"title": "must be at most 10 characters"}},
		{"wrong type", `{"title":123,"author_id":"x"}`, "validation_failed", map[string]string{"title": "must be a string"}},
		{"malformed", `{"title":`, "invalid_body", map[string]string{}},
		{"empty body", ``, "invalid_body", map[string]string{}},
		{"not an object", `[]`, "invalid_body", map[string]string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(r, http.MethodPost, "/articles", tc.body)
			resp := decodeError(t, w)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, tc.code, resp.Error.Code)

			got := map[string]string{}
			for _, f := range resp.Error.Fields {
				got[f.Field] = f.Message
			}
			assert.Equal(t, tc.fields, got)
		})
	}

	t.Run("valid body", func(t *testing.T) {
		w := serve(r, http.MethodPost, "/articles", `{"title":"hello","author_id":"x"}`)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.JSONEq(t, `{"data":{"title":"hello","author_id":"x"}}`, w.Body.String())
	})
}

func TestDomainValidationErrorBecomes400WithFields(t *testing.T) {
	r := newRouter()
	r.POST("/articles", func(c *gin.Context) {
		httpx.Error(c, fmt.Errorf("create article: %w", &apperr.ValidationError{Fields: []apperr.FieldError{
			{Field: "title", Message: "is required"},
			{Field: "body", Message: "must be at most 100000 characters"},
		}}))
	})

	w := serve(r, http.MethodPost, "/articles", `{}`)
	resp := decodeError(t, w)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "validation_failed", resp.Error.Code)
	require.Len(t, resp.Error.Fields, 2)
	assert.Equal(t, "title", resp.Error.Fields[0].Field)
	assert.Equal(t, "is required", resp.Error.Fields[0].Message)
	assert.Equal(t, "body", resp.Error.Fields[1].Field)
}

func TestBindJSONRejectsOversizedBodyWith413(t *testing.T) {
	r := newRouter()
	r.POST("/articles", httpx.MaxBodyBytes(64), func(c *gin.Context) {
		var req createRequest
		if httpx.BindJSON(c, &req) {
			httpx.Created(c, req)
		}
	})

	w := serve(r, http.MethodPost, "/articles", `{"title":"`+strings.Repeat("a", 200)+`","author_id":"x"}`)
	resp := decodeError(t, w)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Equal(t, "payload_too_large", resp.Error.Code)

	w = serve(r, http.MethodPost, "/articles", `{"title":"ok","author_id":"x"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestTimeoutGivesHandlerADeadline(t *testing.T) {
	r := newRouter()
	r.GET("/deadline", httpx.Timeout(time.Second), func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		httpx.OK(c, ok && time.Until(deadline) <= time.Second)
	})
	r.GET("/none", func(c *gin.Context) {
		_, ok := c.Request.Context().Deadline()
		httpx.OK(c, ok)
	})

	assert.JSONEq(t, `{"data":true}`, serve(r, http.MethodGet, "/deadline", "").Body.String())
	assert.JSONEq(t, `{"data":false}`, serve(r, http.MethodGet, "/none", "").Body.String())
}

func TestRouterFallbacksUseEnvelope(t *testing.T) {
	captureLogs(t)

	r := newRouter()
	r.GET("/articles", func(c *gin.Context) { httpx.OK(c, "ok") })
	r.GET("/panic", func(c *gin.Context) { panic("nil map write in handler") })

	cases := []struct {
		method, target string
		status         int
		code           string
	}{
		{http.MethodGet, "/nope", http.StatusNotFound, "route_not_found"},
		{http.MethodDelete, "/articles", http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodGet, "/panic", http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			w := serve(r, tc.method, tc.target, "")
			resp := decodeError(t, w)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, resp.Error.Code)
			assert.NotContains(t, w.Body.String(), "nil map")
		})
	}
}

func TestRequestIDHeader(t *testing.T) {
	r := newRouter()
	r.GET("/x", func(c *gin.Context) { httpx.OK(c, httpx.RequestIDFrom(c)) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(httpx.RequestIDHeader, "lb-abc.123_x")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, "lb-abc.123_x", w.Header().Get(httpx.RequestIDHeader))

	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(httpx.RequestIDHeader, `bad id" level=ERROR`)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	got := w.Header().Get(httpx.RequestIDHeader)
	assert.NotEmpty(t, got)
	assert.Regexp(t, `^[A-Za-z0-9._-]{1,64}$`, got)
}
