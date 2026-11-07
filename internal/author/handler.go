package author

import (
	"context"

	"github.com/gin-gonic/gin"

	"kumparan-assessment/internal/httpx"
	"kumparan-assessment/internal/pagination"
)

const listCacheControl = "public, max-age=60, stale-while-revalidate=300"

type authorService interface {
	Create(ctx context.Context, in CreateInput) (Author, error)
	List(ctx context.Context, in ListInput) (ListResult, error)
}

type Handler struct {
	svc authorService
}

func NewHandler(svc authorService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(r gin.IRouter) {
	r.POST("/authors", h.create)
	r.GET("/authors", h.list)
}

type createAuthorRequest struct {
	Name string `json:"name"`
}

type authorResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *Handler) create(c *gin.Context) {
	var req createAuthorRequest
	if !httpx.BindJSON(c, &req) {
		return
	}

	a, err := h.svc.Create(c.Request.Context(), CreateInput{Name: req.Name})
	if err != nil {
		httpx.Error(c, err)
		return
	}

	httpx.Created(c, toResponse(a))
}

func (h *Handler) list(c *gin.Context) {
	limit, err := pagination.ParseLimit(c.Query("limit"))
	if err != nil {
		httpx.Error(c, err)
		return
	}

	var afterID string
	if raw := c.Query("cursor"); raw != "" {
		afterID, err = pagination.DecodeID(raw)
		if err != nil {
			httpx.Error(c, err)
			return
		}
	}

	res, err := h.svc.List(c.Request.Context(), ListInput{
		Name:    c.Query("name"),
		Limit:   limit,
		AfterID: afterID,
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}

	items := make([]authorResponse, 0, len(res.Authors))
	for _, a := range res.Authors {
		items = append(items, toResponse(a))
	}

	c.Header("Cache-Control", listCacheControl)
	httpx.List(c, items, res.NextCursor)
}

func toResponse(a Author) authorResponse {
	return authorResponse{ID: a.ID, Name: a.Name}
}
