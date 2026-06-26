package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.temporal.io/api/workflowservice/v1"
	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClusterInfoClient struct {
	getClusterInfoFn func(ctx context.Context, req *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error)
}

func (m *mockClusterInfoClient) GetClusterInfo(ctx context.Context, req *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error) {
	return m.getClusterInfoFn(ctx, req)
}

func newHealthTestEngine(svc clusterInfoClient) *gin.Engine {
	h := newHealthHandler(svc, zap.NewNop())
	r := gin.New()
	r.GET("/api/v1/health/temporal", h.CheckTemporalHealth)
	return r
}

func TestCheckTemporalHealth_Success(t *testing.T) {
	mock := &mockClusterInfoClient{
		getClusterInfoFn: func(_ context.Context, _ *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error) {
			return &workflowservice.GetClusterInfoResponse{
				ClusterName:   "active",
				ServerVersion: "1.24.0",
			}, nil
		},
	}
	r := newHealthTestEngine(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/temporal", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp TemporalHealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, "active", resp.ClusterName)
	assert.Equal(t, "1.24.0", resp.ServerVersion)
}

func TestCheckTemporalHealth_Unavailable(t *testing.T) {
	mock := &mockClusterInfoClient{
		getClusterInfoFn: func(_ context.Context, _ *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	r := newHealthTestEngine(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/temporal", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp TemporalHealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "unavailable", resp.Status)
}
