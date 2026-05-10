package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	enumspb "go.temporal.io/api/enums/v1"
	replicationpb "go.temporal.io/api/replication/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
)

type PromoteNamespaceRequest struct {
	// Clusters to assign to the namespace replication config. Optional.
	Clusters []string `json:"clusters"`
}

type PromoteNamespaceResponse struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	IsGlobal bool     `json:"is_global"`
	Clusters []string `json:"clusters,omitempty"`
}

// namespaceServiceClient is the subset of workflowservice.WorkflowServiceClient the handler needs.
type namespaceServiceClient interface {
	RegisterNamespace(ctx context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error)
	DescribeNamespace(ctx context.Context, req *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error)
	UpdateNamespace(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error)
}

// workflowServiceWrapper adapts workflowservice.WorkflowServiceClient (which uses variadic grpc.CallOption)
// to the narrower namespaceServiceClient interface.
type workflowServiceWrapper struct {
	ws workflowservice.WorkflowServiceClient
}

func (w *workflowServiceWrapper) RegisterNamespace(ctx context.Context, req *workflowservice.RegisterNamespaceRequest) (*workflowservice.RegisterNamespaceResponse, error) {
	return w.ws.RegisterNamespace(ctx, req)
}

func (w *workflowServiceWrapper) DescribeNamespace(ctx context.Context, req *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
	return w.ws.DescribeNamespace(ctx, req)
}

func (w *workflowServiceWrapper) UpdateNamespace(ctx context.Context, req *workflowservice.UpdateNamespaceRequest) (*workflowservice.UpdateNamespaceResponse, error) {
	return w.ws.UpdateNamespace(ctx, req)
}

type NamespaceHandler struct {
	svc    namespaceServiceClient
	logger *zap.Logger
}

func NewNamespaceHandler(c client.Client, logger *zap.Logger) *NamespaceHandler {
	return newNamespaceHandler(&workflowServiceWrapper{ws: c.WorkflowService()}, logger)
}

func newNamespaceHandler(svc namespaceServiceClient, logger *zap.Logger) *NamespaceHandler {
	return &NamespaceHandler{svc: svc, logger: logger}
}

type CreateNamespaceRequest struct {
	Name          string            `json:"name" binding:"required"`
	Description   string            `json:"description"`
	OwnerEmail    string            `json:"owner_email"`
	RetentionDays int32             `json:"retention_days"`
	IsGlobal      bool              `json:"is_global"`
	ActiveCluster string            `json:"active_cluster"`
	Data          map[string]string `json:"data"`
}

type CreateNamespaceResponse struct {
	ID string `json:"id"`
}

var tracer = otel.Tracer("temporal-utility/handlers")

func (h *NamespaceHandler) CreateNamespace(c *gin.Context) {
	ctx, span := tracer.Start(c.Request.Context(), "NamespaceHandler.CreateNamespace")
	defer span.End()

	var req CreateNamespaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	span.SetAttributes(attribute.String("namespace.name", req.Name))
	h.logger.Info("creating namespace", zap.String("namespace", req.Name), zap.String("owner", req.OwnerEmail))

	retentionDays := req.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 7
	}

	if err := h.registerNamespace(ctx, req, retentionDays); err != nil {
		var alreadyExists *serviceerror.NamespaceAlreadyExists
		if errors.As(err, &alreadyExists) {
			h.logger.Warn("namespace already exists", zap.String("namespace", req.Name))
			c.JSON(http.StatusConflict, gin.H{"error": "namespace already exists"})
			return
		}
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("failed to create namespace", zap.String("namespace", req.Name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create namespace"})
		return
	}

	descResp, err := h.svc.DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{
		Namespace: req.Name,
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("failed to describe namespace after creation", zap.String("namespace", req.Name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "namespace created but failed to retrieve ID"})
		return
	}

	id := descResp.GetNamespaceInfo().GetId()
	h.logger.Info("namespace created", zap.String("namespace", req.Name), zap.String("id", id))
	c.JSON(http.StatusCreated, CreateNamespaceResponse{ID: id})
}

// PromoteNamespace promotes a local namespace to a global namespace and optionally
// sets its cluster replication configuration.
// POST /api/v1/namespaces/:name/promote
func (h *NamespaceHandler) PromoteNamespace(c *gin.Context) {
	name := c.Param("name")

	ctx, span := tracer.Start(c.Request.Context(), "NamespaceHandler.PromoteNamespace")
	defer span.End()

	var req PromoteNamespaceRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		span.SetStatus(codes.Error, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	span.SetAttributes(attribute.String("namespace.name", name))
	h.logger.Info("promoting namespace to global",
		zap.String("namespace", name),
		zap.Strings("clusters", req.Clusters),
	)

	updateReq := &workflowservice.UpdateNamespaceRequest{
		Namespace:        name,
		PromoteNamespace: true,
	}

	if len(req.Clusters) > 0 {
		clusters := make([]*replicationpb.ClusterReplicationConfig, len(req.Clusters))
		for i, name := range req.Clusters {
			clusters[i] = &replicationpb.ClusterReplicationConfig{ClusterName: name}
		}
		updateReq.ReplicationConfig = &replicationpb.NamespaceReplicationConfig{
			Clusters: clusters,
		}
	}

	resp, err := h.svc.UpdateNamespace(ctx, updateReq)
	if err != nil {
		var notFound *serviceerror.NamespaceNotFound
		if errors.As(err, &notFound) {
			h.logger.Warn("namespace not found", zap.String("namespace", name))
			c.JSON(http.StatusNotFound, gin.H{"error": "namespace not found"})
			return
		}
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("failed to promote namespace", zap.String("namespace", name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to promote namespace"})
		return
	}

	h.logger.Info("namespace promoted to global", zap.String("namespace", name))

	respClusters := make([]string, 0, len(resp.GetReplicationConfig().GetClusters()))
	for _, cl := range resp.GetReplicationConfig().GetClusters() {
		respClusters = append(respClusters, cl.GetClusterName())
	}

	c.JSON(http.StatusOK, PromoteNamespaceResponse{
		ID:       resp.GetNamespaceInfo().GetId(),
		Name:     resp.GetNamespaceInfo().GetName(),
		IsGlobal: resp.GetIsGlobalNamespace(),
		Clusters: respClusters,
	})
}

func (h *NamespaceHandler) registerNamespace(ctx context.Context, req CreateNamespaceRequest, retentionDays int32) error {
	registerReq := &workflowservice.RegisterNamespaceRequest{
		Namespace:                        req.Name,
		Description:                      req.Description,
		OwnerEmail:                       req.OwnerEmail,
		WorkflowExecutionRetentionPeriod: durationpb.New(time.Duration(retentionDays) * 24 * time.Hour),
		HistoryArchivalState:             enumspb.ARCHIVAL_STATE_DISABLED,
		VisibilityArchivalState:          enumspb.ARCHIVAL_STATE_DISABLED,
		IsGlobalNamespace:                req.IsGlobal,
		Data:                             req.Data,
	}

	if req.ActiveCluster != "" {
		registerReq.ActiveClusterName = req.ActiveCluster
	}

	_, err := h.svc.RegisterNamespace(ctx, registerReq)
	return err
}
