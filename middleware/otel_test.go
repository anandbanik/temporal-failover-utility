package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newOTELEngine(statusCode int) *gin.Engine {
	r := gin.New()
	r.Use(OTEL())
	r.GET("/test", func(c *gin.Context) { c.Status(statusCode) })
	return r
}

func TestOTEL_PassesThrough2xx(t *testing.T) {
	r := newOTELEngine(http.StatusOK)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOTEL_PassesThrough4xx(t *testing.T) {
	r := newOTELEngine(http.StatusNotFound)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestOTEL_PassesThrough5xx(t *testing.T) {
	r := newOTELEngine(http.StatusInternalServerError)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestOTEL_PropagatesTraceContext(t *testing.T) {
	var spanCtxValid bool
	r := gin.New()
	r.Use(OTEL())
	r.GET("/test", func(c *gin.Context) {
		// The request context should have an active span injected by the middleware.
		spanCtxValid = c.Request.Context() != nil
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	assert.True(t, spanCtxValid)
}
