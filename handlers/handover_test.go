package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	replicationpb "go.temporal.io/api/replication/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHandoverServiceClient struct {
	updateFn  func(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error)
	startWFFn func(ctx context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error)
}

func (m *mockHandoverServiceClient) UpdateNamespace(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
	return m.updateFn(ctx, req)
}

func (m *mockHandoverServiceClient) StartWorkflowExecution(ctx context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
	return m.startWFFn(ctx, req)
}

func newHandoverTestEngine(svc handoverServiceClient) *gin.Engine {
	h := newHandoverHandler(svc, zap.NewNop())
	r := gin.New()
	r.POST("/api/v1/namespaces/:name/handover", h.Handover)
	return r
}

func postHandover(r *gin.Engine, namespace string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/"+namespace+"/handover", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func okHandoverMock(runID string) *mockHandoverServiceClient {
	return &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return &workflowservice.UpdateNamespaceResponse{}, nil
		},
		startWFFn: func(_ context.Context, _ *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			return &workflowservice.StartWorkflowExecutionResponse{RunId: runID}, nil
		},
	}
}

func TestHandover_Success(t *testing.T) {
	r := newHandoverTestEngine(okHandoverMock("run-001"))

	w := postHandover(r, "replicationtest", map[string]any{"cluster": "btWest"})

	require.Equal(t, http.StatusAccepted, w.Code)
	var resp HandoverResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.WorkflowID)
	assert.Equal(t, "run-001", resp.RunID)
}

func TestHandover_WorkflowIDContainsNamespace(t *testing.T) {
	r := newHandoverTestEngine(okHandoverMock("run-001"))

	w := postHandover(r, "myns", map[string]any{"cluster": "btWest"})

	require.Equal(t, http.StatusAccepted, w.Code)
	var resp HandoverResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, strings.HasPrefix(resp.WorkflowID, "namespace-handover-myns-"))
}

func TestHandover_MissingCluster(t *testing.T) {
	r := newHandoverTestEngine(okHandoverMock(""))

	w := postHandover(r, "myns", map[string]any{})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandover_InvalidJSON(t *testing.T) {
	r := newHandoverTestEngine(okHandoverMock(""))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/namespaces/myns/handover", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandover_NamespaceNotFound(t *testing.T) {
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return nil, serviceerror.NewNamespaceNotFound("myns")
		},
	}
	r := newHandoverTestEngine(mock)

	w := postHandover(r, "myns", map[string]any{"cluster": "btWest"})

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandover_UpdateError(t *testing.T) {
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return nil, fmt.Errorf("temporal unavailable")
		},
	}
	r := newHandoverTestEngine(mock)

	w := postHandover(r, "myns", map[string]any{"cluster": "btWest"})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to update active cluster")
}

func TestHandover_WorkflowStartError(t *testing.T) {
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return &workflowservice.UpdateNamespaceResponse{}, nil
		},
		startWFFn: func(_ context.Context, _ *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			return nil, fmt.Errorf("workflow service error")
		},
	}
	r := newHandoverTestEngine(mock)

	w := postHandover(r, "myns", map[string]any{"cluster": "btWest"})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to start handover workflow")
}

func TestHandover_UpdateActiveClusterRequest(t *testing.T) {
	var capturedUpdate *workflowservice.UpdateNamespaceRequest
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			capturedUpdate = req
			return &workflowservice.UpdateNamespaceResponse{}, nil
		},
		startWFFn: func(_ context.Context, _ *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			return &workflowservice.StartWorkflowExecutionResponse{RunId: "r1"}, nil
		},
	}
	r := newHandoverTestEngine(mock)

	postHandover(r, "replicationtest", map[string]any{"cluster": "btWest"})

	require.NotNil(t, capturedUpdate)
	assert.Equal(t, "replicationtest", capturedUpdate.Namespace)
	assert.Equal(t, "btWest", capturedUpdate.ReplicationConfig.ActiveClusterName)
}

func TestHandover_WorkflowExecutionRequest(t *testing.T) {
	var capturedStart *workflowservice.StartWorkflowExecutionRequest
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return &workflowservice.UpdateNamespaceResponse{}, nil
		},
		startWFFn: func(_ context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			capturedStart = req
			return &workflowservice.StartWorkflowExecutionResponse{RunId: "r1"}, nil
		},
	}
	r := newHandoverTestEngine(mock)

	postHandover(r, "replicationtest", map[string]any{"cluster": "c2"})

	require.NotNil(t, capturedStart)
	assert.Equal(t, handoverSystemNS, capturedStart.Namespace)
	assert.Equal(t, handoverWorkflowType, capturedStart.WorkflowType.GetName())
	assert.Equal(t, handoverTaskQueue, capturedStart.TaskQueue.GetName())
	assert.NotEmpty(t, capturedStart.RequestId)
}

func TestHandover_WorkflowInputEncoding(t *testing.T) {
	var capturedStart *workflowservice.StartWorkflowExecutionRequest
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return &workflowservice.UpdateNamespaceResponse{}, nil
		},
		startWFFn: func(_ context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			capturedStart = req
			return &workflowservice.StartWorkflowExecutionResponse{RunId: "r1"}, nil
		},
	}
	r := newHandoverTestEngine(mock)

	postHandover(r, "replicationtest", map[string]any{"cluster": "c2"})

	require.NotNil(t, capturedStart)
	require.NotNil(t, capturedStart.Input)
	require.Len(t, capturedStart.Input.Payloads, 1)

	var decoded handoverWorkflowInput
	err := json.Unmarshal(capturedStart.Input.Payloads[0].Data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "replicationtest", decoded.Namespace)
	assert.Equal(t, "c2", decoded.RemoteCluster)
	assert.Equal(t, handoverLaggingSeconds, decoded.AllowedLaggingSeconds)
	assert.Equal(t, handoverTimeoutSeconds, decoded.HandoverTimeoutSeconds)
}

func TestHandover_NoWorkflowStartedOnUpdateFailure(t *testing.T) {
	workflowStarted := false
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, _ *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			return nil, fmt.Errorf("update failed")
		},
		startWFFn: func(_ context.Context, _ *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			workflowStarted = true
			return &workflowservice.StartWorkflowExecutionResponse{}, nil
		},
	}
	r := newHandoverTestEngine(mock)

	postHandover(r, "myns", map[string]any{"cluster": "btWest"})

	assert.False(t, workflowStarted, "workflow must not start when active cluster update fails")
}

func TestEncodeHandoverInput_RoundTrip(t *testing.T) {
	input := handoverWorkflowInput{
		Namespace:              "my-ns",
		RemoteCluster:          "cluster-b",
		AllowedLaggingSeconds:  120,
		HandoverTimeoutSeconds: 5,
	}
	payloads, err := encodeHandoverInput(input)
	require.NoError(t, err)
	require.Len(t, payloads.Payloads, 1)

	var decoded handoverWorkflowInput
	require.NoError(t, json.Unmarshal(payloads.Payloads[0].Data, &decoded))
	assert.Equal(t, input, decoded)
}

func TestHandover_ActiveClusterNotUpdatedOnReplicationConfigCheck(t *testing.T) {
	var capturedUpdate *workflowservice.UpdateNamespaceRequest
	mock := &mockHandoverServiceClient{
		updateFn: func(_ context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
			capturedUpdate = req
			return &workflowservice.UpdateNamespaceResponse{}, nil
		},
		startWFFn: func(_ context.Context, _ *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
			return &workflowservice.StartWorkflowExecutionResponse{RunId: "r1"}, nil
		},
	}
	r := newHandoverTestEngine(mock)

	postHandover(r, "myns", map[string]any{"cluster": "btWest"})

	require.NotNil(t, capturedUpdate)
	// Only ActiveClusterName should be set; Clusters list must not be touched.
	assert.Nil(t, capturedUpdate.ReplicationConfig.Clusters)
	assert.False(t, capturedUpdate.PromoteNamespace)

	// Confirm the replication update only targets the active cluster field.
	rCfg := &replicationpb.NamespaceReplicationConfig{ActiveClusterName: "btWest"}
	assert.Equal(t, rCfg, capturedUpdate.ReplicationConfig)
}
