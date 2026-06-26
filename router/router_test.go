package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHealthz(t *testing.T) {
	r := New(nil, nil, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestNamespaceRouteRegistered(t *testing.T) {
	r := New(nil, nil, nil, nil, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// A registered route with a nil handler will panic (recovered to 500),
	// but an unregistered route returns 404. Either way we verify routing, not handler behaviour.
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}
