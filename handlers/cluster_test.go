package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.temporal.io/api/operatorservice/v1"
	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClusterServiceClient struct {
	upsertFn func(ctx context.Context, req *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error)
}

func (m *mockClusterServiceClient) AddOrUpdateRemoteCluster(ctx context.Context, req *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
	return m.upsertFn(ctx, req)
}

func newClusterTestEngine(svc clusterServiceClient) *gin.Engine {
	h := newClusterHandler(svc, zap.NewNop())
	r := gin.New()
	r.POST("/api/v1/clusters", h.UpsertRemoteCluster)
	return r
}

func postCluster(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func okClusterMock() *mockClusterServiceClient {
	return &mockClusterServiceClient{
		upsertFn: func(_ context.Context, _ *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
			return &operatorservice.AddOrUpdateRemoteClusterResponse{}, nil
		},
	}
}

func TestUpsertRemoteCluster_Success(t *testing.T) {
	r := newClusterTestEngine(okClusterMock())

	w := postCluster(r, map[string]any{
		"frontend_address":  "temporal-east:7233",
		"enable_connection": true,
	})

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestUpsertRemoteCluster_MissingFrontendAddress(t *testing.T) {
	r := newClusterTestEngine(okClusterMock())

	w := postCluster(r, map[string]any{"enable_connection": true})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpsertRemoteCluster_InvalidJSON(t *testing.T) {
	r := newClusterTestEngine(okClusterMock())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpsertRemoteCluster_TemporalError(t *testing.T) {
	mock := &mockClusterServiceClient{
		upsertFn: func(_ context.Context, _ *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
			return nil, fmt.Errorf("temporal unavailable")
		},
	}
	r := newClusterTestEngine(mock)

	w := postCluster(r, map[string]any{"frontend_address": "temporal-east:7233"})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to upsert remote cluster")
}

func TestUpsertRemoteCluster_RequestFields(t *testing.T) {
	var captured *operatorservice.AddOrUpdateRemoteClusterRequest
	mock := &mockClusterServiceClient{
		upsertFn: func(_ context.Context, req *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
			captured = req
			return &operatorservice.AddOrUpdateRemoteClusterResponse{}, nil
		},
	}
	r := newClusterTestEngine(mock)

	postCluster(r, map[string]any{
		"frontend_address":      "temporal-east:7233",
		"enable_connection":     true,
		"frontend_http_address": "temporal-east:7243",
		"enable_replication":    true,
	})

	require.NotNil(t, captured)
	assert.Equal(t, "temporal-east:7233", captured.FrontendAddress)
	assert.True(t, captured.EnableRemoteClusterConnection)
	assert.Equal(t, "temporal-east:7243", captured.FrontendHttpAddress)
	assert.True(t, captured.EnableReplication)
}

func TestUpsertRemoteCluster_DefaultsToConnectionDisabled(t *testing.T) {
	var captured *operatorservice.AddOrUpdateRemoteClusterRequest
	mock := &mockClusterServiceClient{
		upsertFn: func(_ context.Context, req *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
			captured = req
			return &operatorservice.AddOrUpdateRemoteClusterResponse{}, nil
		},
	}
	r := newClusterTestEngine(mock)

	postCluster(r, map[string]any{"frontend_address": "temporal-east:7233"})

	require.NotNil(t, captured)
	assert.False(t, captured.EnableRemoteClusterConnection)
}
