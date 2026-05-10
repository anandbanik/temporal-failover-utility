package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	namespacepb "go.temporal.io/api/namespace/v1"
	replicationpb "go.temporal.io/api/replication/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockNamespaceClient implements namespaceServiceClient for testing.
type mockNamespaceClient struct {
	registerFn func(ctx context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error)
	describeFn func(ctx context.Context, req *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error)
	updateFn   func(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error)
}

func (m *mockNamespaceClient) RegisterNamespace(ctx context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
	return m.registerFn(ctx, req)
}

func (m *mockNamespaceClient) DescribeNamespace(ctx context.Context, req *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
	return m.describeFn(ctx, req)
}

func (m *mockNamespaceClient) UpdateNamespace(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
	return m.updateFn(ctx, req)
}

func successMock(namespaceID string) *mockNamespaceClient {
	return &mockNamespaceClient{
		registerFn: func(_ context.Context, _ *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			return &workflowservice.RegisterNamespaceResponse{}, nil
		},
		describeFn: func(_ context.Context, _ *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
			return &workflowservice.DescribeNamespaceResponse{
				NamespaceInfo: &namespacepb.NamespaceInfo{Id: namespaceID},
			}, nil
		},
	}
}

func newTestEngine(svc namespaceServiceClient) *gin.Engine {
	h := newNamespaceHandler(svc, zap.NewNop())
	r := gin.New()
	r.POST("/api/v1/namespaces", h.CreateNamespace)
	r.POST("/api/v1/namespaces/:name/promote", h.PromoteNamespace)
	return r
}

func promoteNamespace(r *gin.Engine, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/"+name+"/promote", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func postNamespace(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateNamespace_Success(t *testing.T) {
	r := newTestEngine(successMock("ns-abc-123"))

	w := postNamespace(r, map[string]any{"name": "my-ns"})

	require.Equal(t, http.StatusCreated, w.Code)
	var resp CreateNamespaceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ns-abc-123", resp.ID)
}

func TestCreateNamespace_MissingName(t *testing.T) {
	r := newTestEngine(successMock(""))

	w := postNamespace(r, map[string]any{"description": "no name"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateNamespace_InvalidJSON(t *testing.T) {
	r := newTestEngine(successMock(""))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateNamespace_AlreadyExists(t *testing.T) {
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, _ *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			return nil, serviceerror.NewNamespaceAlreadyExists("already exists")
		},
	}
	r := newTestEngine(mock)

	w := postNamespace(r, map[string]any{"name": "existing-ns"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "namespace already exists")
}

func TestCreateNamespace_RegisterError(t *testing.T) {
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, _ *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			return nil, fmt.Errorf("temporal unavailable")
		},
	}
	r := newTestEngine(mock)

	w := postNamespace(r, map[string]any{"name": "my-ns"})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to create namespace")
}

func TestCreateNamespace_DescribeError(t *testing.T) {
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, _ *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			return &workflowservice.RegisterNamespaceResponse{}, nil
		},
		describeFn: func(_ context.Context, _ *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
			return nil, fmt.Errorf("describe failed")
		},
	}
	r := newTestEngine(mock)

	w := postNamespace(r, map[string]any{"name": "my-ns"})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to retrieve ID")
}

func TestCreateNamespace_DefaultRetention(t *testing.T) {
	var capturedReq *workflowservice.RegisterNamespaceRequest
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.RegisterNamespaceResponse{}, nil
		},
		describeFn: func(_ context.Context, _ *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
			return &workflowservice.DescribeNamespaceResponse{
				NamespaceInfo: &namespacepb.NamespaceInfo{Id: "id-1"},
			}, nil
		},
	}
	r := newTestEngine(mock)

	postNamespace(r, map[string]any{"name": "my-ns"})

	require.NotNil(t, capturedReq)
	assert.Equal(t, durationpb.New(7*24*time.Hour), capturedReq.WorkflowExecutionRetentionPeriod)
}

func TestCreateNamespace_CustomRetention(t *testing.T) {
	var capturedReq *workflowservice.RegisterNamespaceRequest
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.RegisterNamespaceResponse{}, nil
		},
		describeFn: func(_ context.Context, _ *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
			return &workflowservice.DescribeNamespaceResponse{
				NamespaceInfo: &namespacepb.NamespaceInfo{Id: "id-1"},
			}, nil
		},
	}
	r := newTestEngine(mock)

	postNamespace(r, map[string]any{"name": "my-ns", "retention_days": 30})

	require.NotNil(t, capturedReq)
	assert.Equal(t, durationpb.New(30*24*time.Hour), capturedReq.WorkflowExecutionRetentionPeriod)
}

func TestCreateNamespace_ZeroRetentionUsesDefault(t *testing.T) {
	var capturedReq *workflowservice.RegisterNamespaceRequest
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.RegisterNamespaceResponse{}, nil
		},
		describeFn: func(_ context.Context, _ *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
			return &workflowservice.DescribeNamespaceResponse{
				NamespaceInfo: &namespacepb.NamespaceInfo{Id: "id-1"},
			}, nil
		},
	}
	r := newTestEngine(mock)

	postNamespace(r, map[string]any{"name": "my-ns", "retention_days": 0})

	require.NotNil(t, capturedReq)
	assert.Equal(t, durationpb.New(7*24*time.Hour), capturedReq.WorkflowExecutionRetentionPeriod)
}

func TestCreateNamespace_ActiveCluster(t *testing.T) {
	var capturedReq *workflowservice.RegisterNamespaceRequest
	mock := &mockNamespaceClient{
		registerFn: func(_ context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.RegisterNamespaceResponse{}, nil
		},
		describeFn: func(_ context.Context, _ *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
			return &workflowservice.DescribeNamespaceResponse{
				NamespaceInfo: &namespacepb.NamespaceInfo{Id: "id-1"},
			}, nil
		},
	}
	r := newTestEngine(mock)

	postNamespace(r, map[string]any{"name": "my-ns", "active_cluster": "us-east"})

	require.NotNil(t, capturedReq)
	assert.Equal(t, "us-east", capturedReq.ActiveClusterName)
}

// --- PromoteNamespace tests ---

func promoteMock(id, name string, isGlobal bool, clusters ...string) *mockNamespaceClient {
	clusterConfigs := make([]*replicationpb.ClusterReplicationConfig, len(clusters))
	for i, c := range clusters {
		clusterConfigs[i] = &replicationpb.ClusterReplicationConfig{ClusterName: c}
	}
	return &mockNamespaceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return &workflowservice.UpdateNamespaceResponse{
				NamespaceInfo:     &namespacepb.NamespaceInfo{Id: id, Name: name},
				IsGlobalNamespace: isGlobal,
				ReplicationConfig: &replicationpb.NamespaceReplicationConfig{Clusters: clusterConfigs},
			}, nil
		},
	}
}

func TestPromoteNamespace_Success(t *testing.T) {
	r := newTestEngine(promoteMock("ns-id-1", "my-ns", true))

	w := promoteNamespace(r, "my-ns")

	require.Equal(t, http.StatusOK, w.Code)
	var resp PromoteNamespaceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ns-id-1", resp.ID)
	assert.Equal(t, "my-ns", resp.Name)
	assert.True(t, resp.IsGlobal)
}

func TestPromoteNamespace_NotFound(t *testing.T) {
	mock := &mockNamespaceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return nil, serviceerror.NewNamespaceNotFound("missing-ns")
		},
	}
	r := newTestEngine(mock)

	w := promoteNamespace(r, "missing-ns")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "namespace not found")
}

func TestPromoteNamespace_UpdateError(t *testing.T) {
	mock := &mockNamespaceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return nil, fmt.Errorf("temporal unavailable")
		},
	}
	r := newTestEngine(mock)

	w := promoteNamespace(r, "my-ns")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to promote namespace")
}

func TestPromoteNamespace_SetsPromoteFlag(t *testing.T) {
	var capturedReq *workflowservice.UpdateNamespaceRequest
	mock := &mockNamespaceClient{
		updateFn: func(_ context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.UpdateNamespaceResponse{
				NamespaceInfo:     &namespacepb.NamespaceInfo{Id: "id-1", Name: "my-ns"},
				IsGlobalNamespace: true,
			}, nil
		},
	}
	r := newTestEngine(mock)

	promoteNamespace(r, "my-ns")

	require.NotNil(t, capturedReq)
	assert.Equal(t, "my-ns", capturedReq.Namespace)
	assert.True(t, capturedReq.PromoteNamespace)
}

func TestPromoteNamespace_WithClusters(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"clusters": []string{"c1", "c2"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/my-ns/promote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newTestEngine(promoteMock("ns-id-1", "my-ns", true, "c1", "c2")).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp PromoteNamespaceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.ElementsMatch(t, []string{"c1", "c2"}, resp.Clusters)
}

func TestPromoteNamespace_ClustersSetInReplicationConfig(t *testing.T) {
	var capturedReq *workflowservice.UpdateNamespaceRequest
	mock := &mockNamespaceClient{
		updateFn: func(_ context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.UpdateNamespaceResponse{
				NamespaceInfo:     &namespacepb.NamespaceInfo{Id: "id-1", Name: "my-ns"},
				IsGlobalNamespace: true,
				ReplicationConfig: &replicationpb.NamespaceReplicationConfig{
					Clusters: []*replicationpb.ClusterReplicationConfig{
						{ClusterName: "c1"},
						{ClusterName: "c2"},
					},
				},
			}, nil
		},
	}
	r := newTestEngine(mock)

	body, _ := json.Marshal(map[string]any{"clusters": []string{"c1", "c2"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/my-ns/promote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.ReplicationConfig)
	clusterNames := make([]string, len(capturedReq.ReplicationConfig.Clusters))
	for i, cl := range capturedReq.ReplicationConfig.Clusters {
		clusterNames[i] = cl.ClusterName
	}
	assert.ElementsMatch(t, []string{"c1", "c2"}, clusterNames)
}

func TestPromoteNamespace_NoClustersSkipsReplicationConfig(t *testing.T) {
	var capturedReq *workflowservice.UpdateNamespaceRequest
	mock := &mockNamespaceClient{
		updateFn: func(_ context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			capturedReq = req
			return &workflowservice.UpdateNamespaceResponse{
				NamespaceInfo:     &namespacepb.NamespaceInfo{Id: "id-1", Name: "my-ns"},
				IsGlobalNamespace: true,
			}, nil
		},
	}
	r := newTestEngine(mock)

	promoteNamespace(r, "my-ns")

	require.NotNil(t, capturedReq)
	assert.Nil(t, capturedReq.ReplicationConfig)
}

func TestPromoteNamespace_InvalidClustersBody(t *testing.T) {
	r := newTestEngine(promoteMock("", "", false))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/my-ns/promote", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
