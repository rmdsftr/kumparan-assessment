package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"kumparan-assessment/internal/apperr"
	"kumparan-assessment/internal/pagination"
)

const statusClientClosedRequest = 499

type mapping struct {
	err     error
	status  int
	code    string
	message string
}

var mappings = []mapping{
	{apperr.ErrAuthorNotFound, http.StatusUnprocessableEntity, "author_not_found", "author does not exist"},
	{apperr.ErrArticleNotFound, http.StatusNotFound, "article_not_found", "article does not exist"},
	{apperr.ErrNoFieldsToUpdate, http.StatusBadRequest, "no_fields_to_update", "at least one of title or body must be provided"},
	{apperr.ErrAuthorTooBroad, http.StatusBadRequest, "author_too_broad", "author filter matches too many authors; refine it or use author_id"},
	{pagination.ErrInvalidCursor, http.StatusBadRequest, "invalid_cursor", "cursor is invalid"},
	{pagination.ErrInvalidLimit, http.StatusBadRequest, "invalid_limit", fmt.Sprintf("limit must be an integer between 1 and %d", pagination.MaxLimit)},
}

func init() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			name, _, _ = strings.Cut(f.Tag.Get("form"), ",")
		}
		if name == "-" {
			return ""
		}
		return name
	})
}

func Error(c *gin.Context, err error) {
	if err == nil {
		return
	}

	if errors.Is(err, context.Canceled) || errors.Is(c.Request.Context().Err(), context.Canceled) {
		slog.Debug("request canceled by client", requestAttrs(c)...)
		c.AbortWithStatus(statusClientClosedRequest)
		return
	}

	var verr *apperr.ValidationError
	if errors.As(err, &verr) {
		writeError(c, http.StatusBadRequest, "validation_failed", "request validation failed", fromDomainFields(verr.Fields))
		return
	}

	for _, m := range mappings {
		if errors.Is(err, m.err) {
			writeError(c, m.status, m.code, m.message, nil)
			return
		}
	}

	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	if errors.Is(err, context.DeadlineExceeded) {
		status, code, message = http.StatusServiceUnavailable, "timeout", "request timed out"
	}
	slog.Error("request failed", append(requestAttrs(c), "status", status, "err", err)...)
	writeError(c, status, code, message, nil)
}

func BindJSON(c *gin.Context, dst any) bool {
	err := c.ShouldBindJSON(dst)
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	var verrs validator.ValidationErrors
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "request body is too large", nil)
	case errors.As(err, &verrs):
		writeError(c, http.StatusBadRequest, "validation_failed", "request validation failed", fieldErrors(verrs))
	case errors.As(err, &typeErr) && typeErr.Field != "":
		writeError(c, http.StatusBadRequest, "validation_failed", "request validation failed", []FieldError{
			{Field: typeErr.Field, Message: "must be " + jsonTypeName(typeErr.Type)},
		})
	default:
		writeError(c, http.StatusBadRequest, "invalid_body", "request body must be valid JSON", nil)
	}
	return false
}

func requestAttrs(c *gin.Context) []any {
	return []any{"request_id", RequestIDFrom(c), "method", c.Request.Method, "path", c.Request.URL.Path}
}

func fromDomainFields(in []apperr.FieldError) []FieldError {
	out := make([]FieldError, 0, len(in))
	for _, f := range in {
		out = append(out, FieldError(f))
	}
	return out
}

func fieldErrors(verrs validator.ValidationErrors) []FieldError {
	out := make([]FieldError, 0, len(verrs))
	for _, fe := range verrs {
		field := fe.Field()
		if _, path, ok := strings.Cut(fe.Namespace(), "."); ok {
			field = path
		}
		out = append(out, FieldError{Field: field, Message: fieldMessage(fe)})
	}
	return out
}

func fieldMessage(fe validator.FieldError) string {
	unit := ""
	switch fe.Kind() {
	case reflect.String:
		unit = " characters"
	case reflect.Slice, reflect.Array, reflect.Map:
		unit = " items"
	}

	switch fe.Tag() {
	case "required":
		return "is required"
	case "min":
		return "must be at least " + fe.Param() + unit
	case "max":
		return "must be at most " + fe.Param() + unit
	case "uuid", "uuid4", "uuid7":
		return "must be a valid UUID"
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	default:
		return "is invalid"
	}
}

func jsonTypeName(t reflect.Type) string {
	if t == nil {
		return "a valid value"
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "an array"
	case reflect.Map, reflect.Struct:
		return "an object"
	default:
		return "a valid value"
	}
}
