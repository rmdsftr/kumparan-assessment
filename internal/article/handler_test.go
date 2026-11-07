package article_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/article"
	"kumparan-assessment/internal/httpx"
	"kumparan-assessment/internal/pagination"
)

func getArticles(r *gin.Engine, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestListArticlesHandler200(t *testing.T) {
	store := newFakeStore()
	store.listRows = []article.Article{
		listArticle("0193f0a0-0000-7000-8000-00000000b009", 1793671200000000),
		listArticle("0193f0a0-0000-7000-8000-00000000b008", 1793671200000000),
	}
	r := newTestRouter(store)

	w := getArticles(r, "/articles?limit=1")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=5, stale-while-revalidate=30", w.Header().Get("Cache-Control"))

	next := pagination.Encode(pagination.Cursor{CreatedAtUnixMicro: 1793671200000000, ID: "0193f0a0-0000-7000-8000-00000000b009"})
	assert.JSONEq(t, `{
		"data": [{
			"id": "0193f0a0-0000-7000-8000-00000000b009",
			"author": {"id": "`+seedAuthorID+`", "name": "`+seedAuthorName+`"},
			"title": "Judul 0193f0a0-0000-7000-8000-00000000b009",
			"body": "Isi 0193f0a0-0000-7000-8000-00000000b009",
			"created_at": "2026-11-03T02:00:00.000000Z",
			"updated_at": "2026-11-03T02:00:00.000000Z"
		}],
		"next_cursor": "`+next+`"
	}`, w.Body.String())

	w = getArticles(r, "/articles?limit=2")
	assert.Equal(t, http.StatusOK, w.Code)
	var page struct {
		Data       []map[string]any `json:"data"`
		NextCursor *string          `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	assert.Len(t, page.Data, 2)
	assert.Nil(t, page.NextCursor, "last page must have next_cursor null")
}

func TestListArticlesHandlerUnknownAuthorIsEmptyList(t *testing.T) {
	w := getArticles(newTestRouter(newFakeStore()), "/articles?author=Tidak%20Ada")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"data":[],"next_cursor":null}`, w.Body.String())
}

func TestListArticlesHandlerPassesFiltersToStore(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(store)
	cursor := pagination.Cursor{CreatedAtUnixMicro: 1793671200000000, ID: "0193f0a0-0000-7000-8000-00000000b009"}

	w := getArticles(r, "/articles?query=regu%20-kuda&author=Levi%20Ackerman&limit=5&cursor="+pagination.Encode(cursor))
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, store.filters, 1)
	assert.Equal(t, article.ListFilter{
		Query:     "regu -kuda",
		AuthorIDs: []string{seedAuthorID},
		After:     &cursor,
		Limit:     6,
	}, store.filters[0])
	assert.Equal(t, []string{"Levi Ackerman"}, store.resolved)
}

func TestListArticlesHandlerErrors(t *testing.T) {
	cases := []struct {
		name   string
		target string
		code   string
		fields map[string]string
	}{
		{"broken cursor", "/articles?cursor=lol", "invalid_cursor", map[string]string{}},
		{"author and author_id", "/articles?author=Levi&author_id=" + seedAuthorID, "validation_failed", map[string]string{"author": "cannot be combined with author_id"}},
		{"non-numeric limit", "/articles?limit=abc", "invalid_limit", map[string]string{}},
		{"zero limit", "/articles?limit=0", "invalid_limit", map[string]string{}},
		{"author and author_id", "/articles?author=Levi&author_id=" + seedAuthorID, "validation_failed", map[string]string{"author": "cannot be combined with author_id"}},
		{"NUL in query", "/articles?query=regu%00", "validation_failed", map[string]string{"query": "must not contain NUL characters"}},
		{"invalid UTF-8 in author", "/articles?author=Levi%FF", "validation_failed", map[string]string{"author": "must be valid UTF-8"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			w := getArticles(newTestRouter(store), tc.target)

			code, fields := decodeError(t, w)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.fields, fields)
			assert.Empty(t, w.Header().Get("Cache-Control"), "errors must not be cacheable")
			assert.Empty(t, store.filters)
		})
	}
}

type errorResponse struct {
	Error struct {
		Code   string `json:"code"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	} `json:"error"`
}

func newTestRouter(store *fakeStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpx.RequestID(), httpx.Recovery(), httpx.Timeout(5*time.Second), httpx.MaxBodyBytes(1<<20))
	article.NewHandler(article.NewService(store, fixedID)).Register(r)
	return r
}

func postArticle(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/articles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestCreateArticleHandler201(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(store)

	w := postArticle(r, `{
		"id": "client-chosen-id",
		"author_id": "`+seedAuthorID+`",
		"title": "  Judul berita  ",
		"body": "Isi berita",
		"created_at": "1999-01-01T00:00:00Z"
	}`)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.JSONEq(t, `{
		"data": {
			"id": "`+fixedArticleID+`",
			"author": {"id": "`+seedAuthorID+`", "name": "`+seedAuthorName+`"},
			"title": "Judul berita",
			"body": "Isi berita",
			"created_at": "2026-11-07T12:50:00.123450Z",
			"updated_at": "2026-11-07T12:50:00.123450Z"
		}
	}`, w.Body.String())
}

func TestCreateArticleHandlerErrors(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		code   string
		fields map[string]string
	}{
		{
			name:   "malformed JSON",
			body:   `{"title":`,
			status: http.StatusBadRequest,
			code:   "invalid_body",
			fields: map[string]string{},
		},
		{
			name:   "empty title",
			body:   `{"author_id":"` + seedAuthorID + `","title":"","body":"Isi"}`,
			status: http.StatusBadRequest,
			code:   "validation_failed",
			fields: map[string]string{"title": "is required"},
		},
		{
			name:   "whitespace title",
			body:   `{"author_id":"` + seedAuthorID + `","title":"   ","body":"Isi"}`,
			status: http.StatusBadRequest,
			code:   "validation_failed",
			fields: map[string]string{"title": "is required"},
		},
		{
			name:   "wrong type",
			body:   `{"author_id":"` + seedAuthorID + `","title":123,"body":"Isi"}`,
			status: http.StatusBadRequest,
			code:   "validation_failed",
			fields: map[string]string{"title": "must be a string"},
		},
		{
			name:   "unknown author",
			body:   `{"author_id":"0193f0a0-0000-7000-8000-00000000ffff","title":"Judul","body":"Isi"}`,
			status: http.StatusUnprocessableEntity,
			code:   "author_not_found",
			fields: map[string]string{},
		},
		{
			name:   "body over 1 MiB",
			body:   `{"author_id":"` + seedAuthorID + `","title":"Judul","body":"` + strings.Repeat("a", 1<<20) + `"}`,
			status: http.StatusRequestEntityTooLarge,
			code:   "payload_too_large",
			fields: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postArticle(newTestRouter(newFakeStore()), tc.body)

			code, fields := decodeError(t, w)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.fields, fields)
		})
	}
}

func TestCreateArticleHandlerDoesNotHitStoreOnBadInput(t *testing.T) {
	store := newFakeStore()
	r := newTestRouter(store)

	postArticle(r, `{"title":`)
	postArticle(r, `{"author_id":"`+seedAuthorID+`","title":"   ","body":"Isi"}`)
	postArticle(r, `{"author_id":"`+seedAuthorID+`","title":"Judul","body":"`+strings.Repeat("a", 1<<20)+`"}`)

	assert.Empty(t, store.calls)
}

func patchArticle(r *gin.Engine, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/articles/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUpdateArticleHandler200(t *testing.T) {
	store := newFakeStore()
	store.articles[fixedArticleID] = listArticle(fixedArticleID, 1793671200000000)
	r := newTestRouter(store)

	w := patchArticle(r, fixedArticleID, `{"title":"  Judul baru  "}`)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{
		"data": {
			"id": "`+fixedArticleID+`",
			"author": {"id": "`+seedAuthorID+`", "name": "`+seedAuthorName+`"},
			"title": "Judul baru",
			"body": "Isi `+fixedArticleID+`",
			"created_at": "2026-11-03T02:00:00.000000Z",
			"updated_at": "2026-11-07T12:50:00.123450Z"
		}
	}`, w.Body.String())
}

func TestUpdateArticleHandlerErrors(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		body   string
		status int
		code   string
		fields map[string]string
	}{
		{"empty object", fixedArticleID, `{}`, http.StatusBadRequest, "no_fields_to_update", map[string]string{}},
		{"null fields count as not sent", fixedArticleID, `{"title":null,"body":null}`, http.StatusBadRequest, "no_fields_to_update", map[string]string{}},
		{"whitespace title", fixedArticleID, `{"title":"   "}`, http.StatusBadRequest, "validation_failed", map[string]string{"title": "is required"}},
		{"wrong type", fixedArticleID, `{"title":123}`, http.StatusBadRequest, "validation_failed", map[string]string{"title": "must be a string"}},
		{
			"read-only fields", fixedArticleID,
			`{"title":"Judul","author_id":"0193f0a0-0000-7000-8000-00000000a003","created_at":"1999-01-01T00:00:00Z"}`,
			http.StatusBadRequest, "validation_failed",
			map[string]string{"author_id": "cannot be changed", "created_at": "cannot be changed"},
		},
		{"read-only field sent as null", fixedArticleID, `{"title":"Judul","id":null}`, http.StatusBadRequest, "validation_failed", map[string]string{"id": "cannot be changed"}},
		{"malformed JSON", fixedArticleID, `{"title":`, http.StatusBadRequest, "invalid_body", map[string]string{}},
		{"body over 1 MiB", fixedArticleID, `{"body":"` + strings.Repeat("a", 1<<20) + `"}`, http.StatusRequestEntityTooLarge, "payload_too_large", map[string]string{}},
		{"unknown id", "0193f0a0-0000-7000-8000-00000000ffff", `{"title":"Judul"}`, http.StatusNotFound, "article_not_found", map[string]string{}},
		{"NUL in id", "b008%00", `{"title":"Judul"}`, http.StatusNotFound, "article_not_found", map[string]string{}},
		{"invalid UTF-8 in id", "b008%FF", `{"title":"Judul"}`, http.StatusNotFound, "article_not_found", map[string]string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			store.articles[fixedArticleID] = listArticle(fixedArticleID, 1793671200000000)

			w := patchArticle(newTestRouter(store), tc.id, tc.body)

			code, fields := decodeError(t, w)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.fields, fields)
			assert.Empty(t, w.Header().Get("Cache-Control"))
			if tc.name != "unknown id" {
				assert.Empty(t, store.patches, "invalid requests must not reach the store")
			}
		})
	}
}

func TestListArticlesHandlerTooBroadAuthor(t *testing.T) {
	store := newFakeStore()
	for i := 0; i <= article.MaxAuthorMatches; i++ {
		store.authors[fmt.Sprintf("id-%03d", i)] = fmt.Sprintf("Levi %02d", i)
	}
	w := getArticles(newTestRouter(store), "/articles?author=Levi")

	code, _ := decodeError(t, w)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "author_too_broad", code)
}
