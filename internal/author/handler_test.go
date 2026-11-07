package author_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/author"
	"kumparan-assessment/internal/httpx"
	"kumparan-assessment/internal/pagination"
)

type errorResponse struct {
	Error struct {
		Code   string `json:"code"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	} `json:"error"`
}

func newTestRouter(store *fakeStore, ids ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpx.RequestID(), httpx.Recovery(), httpx.Timeout(5*time.Second), httpx.MaxBodyBytes(1<<20))
	author.NewHandler(author.NewService(store, sequentialIDs(ids...))).Register(r)
	return r
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

func decodeError(t *testing.T, w *httptest.ResponseRecorder) (string, map[string]string) {
	t.Helper()
	var resp errorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	fields := map[string]string{}
	for _, f := range resp.Error.Fields {
		fields[f.Field] = f.Message
	}
	return resp.Error.Code, fields
}

func TestCreateAuthorHandler201(t *testing.T) {
	r := newTestRouter(&fakeStore{}, "id-1")

	w := serve(r, http.MethodPost, "/authors", `{"name":"  Levi   Ackerman ","id":"client-id"}`)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.JSONEq(t, `{"data":{"id":"id-1","name":"Levi Ackerman"}}`, w.Body.String())
}

func TestCreateAuthorHandlerSameNameTwice(t *testing.T) {
	r := newTestRouter(&fakeStore{}, "id-1", "id-2")

	first := serve(r, http.MethodPost, "/authors", `{"name":"Levi Ackerman"}`)
	second := serve(r, http.MethodPost, "/authors", `{"name":"Levi Ackerman"}`)

	assert.Equal(t, http.StatusCreated, first.Code)
	assert.Equal(t, http.StatusCreated, second.Code, "a duplicate name is allowed, never a 409")
	assert.JSONEq(t, `{"data":{"id":"id-1","name":"Levi Ackerman"}}`, first.Body.String())
	assert.JSONEq(t, `{"data":{"id":"id-2","name":"Levi Ackerman"}}`, second.Body.String())
}

func TestCreateAuthorHandlerErrors(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		code   string
		fields map[string]string
	}{
		{"missing name", `{}`, http.StatusBadRequest, "validation_failed", map[string]string{"name": "is required"}},
		{"whitespace name", `{"name":"   "}`, http.StatusBadRequest, "validation_failed", map[string]string{"name": "is required"}},
		{"wrong type", `{"name":42}`, http.StatusBadRequest, "validation_failed", map[string]string{"name": "must be a string"}},
		{"malformed JSON", `{"name":`, http.StatusBadRequest, "invalid_body", map[string]string{}},
		{"body over 1 MiB", `{"name":"` + strings.Repeat("a", 1<<20) + `"}`, http.StatusRequestEntityTooLarge, "payload_too_large", map[string]string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			w := serve(newTestRouter(store, "id-1"), http.MethodPost, "/authors", tc.body)

			code, fields := decodeError(t, w)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.fields, fields)
			assert.Empty(t, store.created)
		})
	}
}

func TestListAuthorsHandler200(t *testing.T) {
	store := &fakeStore{listRows: leviRows()}
	r := newTestRouter(store)

	w := serve(r, http.MethodGet, "/authors?name=Levi%20Ackerman&limit=1", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=60, stale-while-revalidate=300", w.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{
		"data": [{"id": "`+leviA002+`", "name": "Levi Ackerman"}],
		"next_cursor": "`+pagination.EncodeID(leviA002)+`"
	}`, w.Body.String())

	w = serve(r, http.MethodGet, "/authors?name=Levi%20Ackerman&limit=2", "")
	assert.JSONEq(t, `{
		"data": [
			{"id": "`+leviA002+`", "name": "Levi Ackerman"},
			{"id": "`+leviA003+`", "name": "Levi Ackerman"}
		],
		"next_cursor": null
	}`, w.Body.String())
}

func TestListAuthorsHandlerUnknownNameIsEmptyList(t *testing.T) {
	w := serve(newTestRouter(&fakeStore{}), http.MethodGet, "/authors?name=Tidak%20Ada", "")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"data":[],"next_cursor":null}`, w.Body.String())
}

func TestListAuthorsHandlerPassesNormalisedFilterToStore(t *testing.T) {
	store := &fakeStore{}
	r := newTestRouter(store)

	w := serve(r, http.MethodGet, "/authors?name=%20%20LEVI%20%20%20ACKERMAN%20&limit=5&cursor="+pagination.EncodeID(leviA002), "")
	require.Equal(t, http.StatusOK, w.Code)

	assert.Equal(t, []author.ListFilter{{Name: "LEVI ACKERMAN", AfterID: leviA002, Limit: 6}}, store.filters)
}

func TestListAuthorsHandlerErrors(t *testing.T) {
	articleCursor := pagination.Encode(pagination.Cursor{CreatedAtUnixMicro: 1793671200000000, ID: "0193f0a0-0000-7000-8000-00000000b008"})

	cases := []struct {
		name   string
		target string
		code   string
		fields map[string]string
	}{
		{"broken cursor", "/authors?cursor=lol", "invalid_cursor", map[string]string{}},
		{"article cursor", "/authors?cursor=" + articleCursor, "invalid_cursor", map[string]string{}},
		{"non-numeric limit", "/authors?limit=abc", "invalid_limit", map[string]string{}},
		{"NUL in name", "/authors?name=Levi%00", "validation_failed", map[string]string{"name": "must not contain NUL characters"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			w := serve(newTestRouter(store), http.MethodGet, tc.target, "")

			code, fields := decodeError(t, w)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.fields, fields)
			assert.Empty(t, w.Header().Get("Cache-Control"))
			assert.Empty(t, store.filters)
		})
	}
}
