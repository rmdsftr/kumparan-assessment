package httpx

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	RequestIDHeader = "X-Request-ID"
	requestIDKey    = "request_id"
)

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = rand.Text()
		}
		c.Set(requestIDKey, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

func RequestIDFrom(c *gin.Context) string {
	return c.GetString(requestIDKey)
}

func MaxBodyBytes(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, rec any) {
		Error(c, fmt.Errorf("panic: %v\n%s", rec, debug.Stack()))
	})
}

func NotFound(c *gin.Context) {
	writeError(c, http.StatusNotFound, "route_not_found", "route not found", nil)
}

func MethodNotAllowed(c *gin.Context) {
	writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
}
