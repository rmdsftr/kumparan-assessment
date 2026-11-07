package apperr

import (
	"errors"
	"strings"
)

var (
	ErrAuthorNotFound   = errors.New("author not found")
	ErrArticleNotFound  = errors.New("article not found")
	ErrNoFieldsToUpdate = errors.New("no fields to update")
	ErrAuthorTooBroad   = errors.New("author filter too broad")
)

type FieldError struct {
	Field   string
	Message string
}

type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+" "+f.Message)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
