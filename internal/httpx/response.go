package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type dataEnvelope struct {
	Data any `json:"data"`
}

type listEnvelope struct {
	Data       any     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	RequestID string       `json:"request_id,omitempty"`
	Fields    []FieldError `json:"fields,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, dataEnvelope{Data: data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, dataEnvelope{Data: data})
}

func List[T any](c *gin.Context, items []T, nextCursor string) {
	if items == nil {
		items = make([]T, 0)
	}
	var next *string
	if nextCursor != "" {
		next = &nextCursor
	}
	c.JSON(http.StatusOK, listEnvelope{Data: items, NextCursor: next})
}

func writeError(c *gin.Context, status int, code, message string, fields []FieldError) {
	if c.Writer.Written() {
		c.Abort()
		return
	}
	c.AbortWithStatusJSON(status, errorEnvelope{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: RequestIDFrom(c),
		Fields:    fields,
	}})
}
