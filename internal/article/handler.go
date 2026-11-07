package article

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/httpx"
	"kumparan-assessment/internal/pagination"
)

const (
	timestampLayout  = "2006-01-02T15:04:05.000000Z07:00"
	listCacheControl = "public, max-age=5, stale-while-revalidate=30"
)

type articleService interface {
	Create(ctx context.Context, in CreateInput) (Article, error)
	Update(ctx context.Context, in UpdateInput) (Article, error)
	List(ctx context.Context, in ListInput) (ListResult, error)
}

type Handler struct {
	svc articleService
}

func NewHandler(svc articleService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(r gin.IRouter) {
	r.POST("/articles", h.create)
	r.GET("/articles", h.list)
	r.PATCH("/articles/:id", h.update)
}

type createArticleRequest struct {
	AuthorID string `json:"author_id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

type updateArticleRequest struct {
	Title     *string         `json:"title"`
	Body      *string         `json:"body"`
	ID        json.RawMessage `json:"id"`
	AuthorID  json.RawMessage `json:"author_id"`
	Author    json.RawMessage `json:"author"`
	CreatedAt json.RawMessage `json:"created_at"`
	UpdatedAt json.RawMessage `json:"updated_at"`
}

func (r updateArticleRequest) readOnlyFieldsError() error {
	var fields []apperr.FieldError
	for _, f := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"id", r.ID},
		{"author_id", r.AuthorID},
		{"author", r.Author},
		{"created_at", r.CreatedAt},
		{"updated_at", r.UpdatedAt},
	} {
		if len(f.raw) > 0 {
			fields = append(fields, apperr.FieldError{Field: f.name, Message: "cannot be changed"})
		}
	}
	if len(fields) > 0 {
		return &apperr.ValidationError{Fields: fields}
	}
	return nil
}

type authorResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type articleResponse struct {
	ID        string         `json:"id"`
	Author    authorResponse `json:"author"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

func (h *Handler) create(c *gin.Context) {
	var req createArticleRequest
	if !httpx.BindJSON(c, &req) {
		return
	}

	a, err := h.svc.Create(c.Request.Context(), CreateInput{
		AuthorID: req.AuthorID,
		Title:    req.Title,
		Body:     req.Body,
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}

	httpx.Created(c, toResponse(a))
}

func (h *Handler) update(c *gin.Context) {
	var req updateArticleRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := req.readOnlyFieldsError(); err != nil {
		httpx.Error(c, err)
		return
	}

	a, err := h.svc.Update(c.Request.Context(), UpdateInput{
		ID:    c.Param("id"),
		Title: req.Title,
		Body:  req.Body,
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}

	httpx.OK(c, toResponse(a))
}

func (h *Handler) list(c *gin.Context) {
	limit, err := pagination.ParseLimit(c.Query("limit"))
	if err != nil {
		httpx.Error(c, err)
		return
	}

	var after *pagination.Cursor
	if raw := c.Query("cursor"); raw != "" {
		cur, err := pagination.Decode(raw)
		if err != nil {
			httpx.Error(c, err)
			return
		}
		after = &cur
	}

	res, err := h.svc.List(c.Request.Context(), ListInput{
		Query:    c.Query("query"),
		Author:   c.Query("author"),
		AuthorID: c.Query("author_id"),
		Limit:    limit,
		After:    after,
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}

	items := make([]articleResponse, 0, len(res.Articles))
	for _, a := range res.Articles {
		items = append(items, toResponse(a))
	}

	c.Header("Cache-Control", listCacheControl)
	httpx.List(c, items, res.NextCursor)
}

func toResponse(a Article) articleResponse {
	return articleResponse{
		ID:        a.ID,
		Author:    authorResponse{ID: a.Author.ID, Name: a.Author.Name},
		Title:     a.Title,
		Body:      a.Body,
		CreatedAt: formatTime(a.CreatedAt),
		UpdatedAt: formatTime(a.UpdatedAt),
	}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timestampLayout)
}
