package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	commonpb "go.temporal.io/api/common/v1"
	replicationpb "go.temporal.io/api/replication/v1"
	"go.temporal.io/api/serviceerror"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

const (
	handoverWorkflowType   = "namespace-handover"
	handoverTaskQueue      = "default-worker-tq"
	handoverSystemNS       = "temporal-system"
	handoverLaggingSeconds = 120
	handoverTimeoutSeconds = 5
)

var handoverTracer = otel.Tracer("temporal-utility/handlers")

// handoverServiceClient is the subset of WorkflowServiceClient the handler needs.
type handoverServiceClient interface {
	UpdateNamespace(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error)
	StartWorkflowExecution(ctx context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error)
}

type handoverServiceWrapper struct {
	ws workflowservice.WorkflowServiceClient
}

func (w *handoverServiceWrapper) UpdateNamespace(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
	return w.ws.UpdateNamespace(ctx, req)
}

func (w *handoverServiceWrapper) StartWorkflowExecution(ctx context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
	return w.ws.StartWorkflowExecution(ctx, req)
}

// HandoverHandler handles namespace handover operations.
type HandoverHandler struct {
	svc    handoverServiceClient
	logger *zap.Logger
}

func NewHandoverHandler(c client.Client, logger *zap.Logger) *HandoverHandler {
	return newHandoverHandler(&handoverServiceWrapper{ws: c.WorkflowService()}, logger)
}

func newHandoverHandler(svc handoverServiceClient, logger *zap.Logger) *HandoverHandler {
	return &HandoverHandler{svc: svc, logger: logger}
}

type HandoverRequest struct {
	Cluster string `json:"cluster" binding:"required"`
}

type HandoverResponse struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
}

// handoverWorkflowInput matches the namespace-handover workflow input schema.
type handoverWorkflowInput struct {
	Namespace              string `json:"Namespace"`
	RemoteCluster          string `json:"RemoteCluster"`
	AllowedLaggingSeconds  int    `json:"AllowedLaggingSeconds"`
	HandoverTimeoutSeconds int    `json:"HandoverTimeoutSeconds"`
}

// Handover godoc
//
//	@Summary      Namespace handover
//	@Description  Updates the active cluster of a namespace, then starts the namespace-handover workflow in temporal-system.
//	@Tags         namespaces
//	@Accept       json
//	@Produce      json
//	@Param        name     path      string           true  "Namespace name"
//	@Param        request  body      HandoverRequest  true  "Target cluster"
//	@Success      202      {object}  HandoverResponse
//	@Failure      400      {object}  map[string]string
//	@Failure      404      {object}  map[string]string
//	@Failure      500      {object}  map[string]string
//	@Router       /api/v1/namespaces/{name}/handover [post]
func (h *HandoverHandler) Handover(c *gin.Context) {
	namespace := c.Param("name")

	ctx, span := handoverTracer.Start(c.Request.Context(), "HandoverHandler.Handover")
	defer span.End()

	var req HandoverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	span.SetAttributes(
		attribute.String("namespace", namespace),
		attribute.String("cluster", req.Cluster),
	)

	// Step 1: update active cluster.
	h.logger.Info("updating active cluster",
		zap.String("namespace", namespace),
		zap.String("cluster", req.Cluster),
	)
	if err := h.updateActiveCluster(ctx, namespace, req.Cluster); err != nil {
		var notFound *serviceerror.NamespaceNotFound
		if errors.As(err, &notFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "namespace not found"})
			return
		}
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("failed to update active cluster",
			zap.String("namespace", namespace),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update active cluster"})
		return
	}
	h.logger.Info("active cluster updated",
		zap.String("namespace", namespace),
		zap.String("cluster", req.Cluster),
	)

	// Step 2: start namespace-handover workflow.
	workflowID := fmt.Sprintf("namespace-handover-%s-%s", namespace, uuid.New().String())
	h.logger.Info("starting namespace-handover workflow",
		zap.String("namespace", namespace),
		zap.String("workflow_id", workflowID),
		zap.String("remote_cluster", req.Cluster),
	)

	runID, err := h.startHandoverWorkflow(ctx, namespace, req.Cluster, workflowID)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("active cluster updated but failed to start handover workflow",
			zap.String("namespace", namespace),
			zap.String("workflow_id", workflowID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "active cluster updated but failed to start handover workflow"})
		return
	}

	h.logger.Info("namespace handover workflow started",
		zap.String("workflow_id", workflowID),
		zap.String("run_id", runID),
	)
	c.JSON(http.StatusAccepted, HandoverResponse{
		WorkflowID: workflowID,
		RunID:      runID,
	})
}

func (h *HandoverHandler) updateActiveCluster(ctx context.Context, namespace, cluster string) error {
	_, err := h.svc.UpdateNamespace(ctx, &workflowservice.UpdateNamespaceRequest{
		Namespace: namespace,
		ReplicationConfig: &replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: cluster,
		},
	})
	return err
}

func (h *HandoverHandler) startHandoverWorkflow(ctx context.Context, namespace, remoteCluster, workflowID string) (string, error) {
	input := handoverWorkflowInput{
		Namespace:              namespace,
		RemoteCluster:          remoteCluster,
		AllowedLaggingSeconds:  handoverLaggingSeconds,
		HandoverTimeoutSeconds: handoverTimeoutSeconds,
	}

	payload, err := converter.GetDefaultDataConverter().ToPayloads(input)
	if err != nil {
		return "", fmt.Errorf("encoding workflow input: %w", err)
	}

	resp, err := h.svc.StartWorkflowExecution(ctx, &workflowservice.StartWorkflowExecutionRequest{
		Namespace:    handoverSystemNS,
		WorkflowId:   workflowID,
		WorkflowType: &commonpb.WorkflowType{Name: handoverWorkflowType},
		TaskQueue:    &taskqueuepb.TaskQueue{Name: handoverTaskQueue},
		Input:        payload,
		RequestId:    uuid.New().String(),
	})
	if err != nil {
		return "", err
	}

	return resp.GetRunId(), nil
}

// encodeHandoverInput is extracted for testability.
func encodeHandoverInput(input handoverWorkflowInput) (*commonpb.Payloads, error) {
	return converter.GetDefaultDataConverter().ToPayloads(input)
}
