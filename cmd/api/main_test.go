package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/testdb"
)

type authorJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type articleJSON struct {
	ID        string     `json:"id"`
	Author    authorJSON `json:"author"`
	Title     string     `json:"title"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

func call(t *testing.T, r *gin.Engine, method, target, body string, wantStatus int, out any) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, wantStatus, w.Code, "%s %s: %s", method, target, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), out))
}

func TestAuthorToArticleFlow(t *testing.T) {
	pool := testdb.Pool(t)
	gin.SetMode(gin.TestMode)
	r := newRouter(pool)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	name := "Penulis E2E " + suffix
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM articles WHERE author_id IN (SELECT id FROM authors WHERE name = $1)`, name)
		_, _ = pool.Exec(ctx, `DELETE FROM authors WHERE name = $1`, name)
	})

	var created struct {
		Data authorJSON `json:"data"`
	}
	call(t, r, http.MethodPost, "/authors", `{"name":"  Penulis   E2E `+suffix+` "}`, http.StatusCreated, &created)
	assert.Equal(t, name, created.Data.Name, "name is stored with whitespace collapsed")

	var posted struct {
		Data articleJSON `json:"data"`
	}
	call(t, r, http.MethodPost, "/articles",
		`{"author_id":"`+created.Data.ID+`","title":"Artikel e2e `+suffix+`","body":"Isi artikel e2e"}`,
		http.StatusCreated, &posted)
	assert.Equal(t, created.Data, posted.Data.Author)

	var byName struct {
		Data []articleJSON `json:"data"`
	}
	sloppyName := "  PENULIS   e2e  " + strings.ToUpper(suffix) + " "
	call(t, r, http.MethodGet, "/articles?author="+url.QueryEscape(sloppyName), "", http.StatusOK, &byName)
	require.Len(t, byName.Data, 1)
	assert.Equal(t, posted.Data.ID, byName.Data[0].ID)

	var byID struct {
		Data []articleJSON `json:"data"`
	}
	call(t, r, http.MethodGet, "/articles?author_id="+url.QueryEscape(created.Data.ID), "", http.StatusOK, &byID)
	require.Len(t, byID.Data, 1)
	assert.Equal(t, posted.Data.ID, byID.Data[0].ID)

	var patched struct {
		Data articleJSON `json:"data"`
	}
	call(t, r, http.MethodPatch, "/articles/"+posted.Data.ID, `{"title":"Artikel e2e diedit `+suffix+`"}`, http.StatusOK, &patched)
	assert.Equal(t, "Artikel e2e diedit "+suffix, patched.Data.Title)
	assert.Equal(t, posted.Data.CreatedAt, patched.Data.CreatedAt, "created_at is immutable")
	assert.Greater(t, patched.Data.UpdatedAt, posted.Data.UpdatedAt, "updated_at moves forward")

	var afterEdit struct {
		Data []articleJSON `json:"data"`
	}
	call(t, r, http.MethodGet, "/articles?author_id="+url.QueryEscape(created.Data.ID), "", http.StatusOK, &afterEdit)
	require.Len(t, afterEdit.Data, 1)
	assert.Equal(t, patched.Data, afterEdit.Data[0])

	var authors struct {
		Data []authorJSON `json:"data"`
	}
	call(t, r, http.MethodGet, "/authors?name="+url.QueryEscape(strings.ToLower(name)), "", http.StatusOK, &authors)
	assert.Equal(t, []authorJSON{created.Data}, authors.Data)
}
