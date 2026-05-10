package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

var clusterTracer = otel.Tracer("temporal-utility/handlers")

type clusterServiceClient interface {
	AddOrUpdateRemoteCluster(ctx context.Context, req *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error)
}

type operatorServiceWrapper struct {
	os operatorservice.OperatorServiceClient
}

func (w *operatorServiceWrapper) AddOrUpdateRemoteCluster(ctx context.Context, req *operatorservice.AddOrUpdateRemoteClusterRequest) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
	return w.os.AddOrUpdateRemoteCluster(ctx, req)
}

type ClusterHandler struct {
	svc    clusterServiceClient
	logger *zap.Logger
}

func NewClusterHandler(c client.Client, logger *zap.Logger) *ClusterHandler {
	return newClusterHandler(&operatorServiceWrapper{os: c.OperatorService()}, logger)
}

func newClusterHandler(svc clusterServiceClient, logger *zap.Logger) *ClusterHandler {
	return &ClusterHandler{svc: svc, logger: logger}
}

type UpsertRemoteClusterRequest struct {
	FrontendAddress      string `json:"frontend_address" binding:"required"`
	EnableConnection     bool   `json:"enable_connection"`
	FrontendHttpAddress  string `json:"frontend_http_address"`
	EnableReplication    bool   `json:"enable_replication"`
}

// UpsertRemoteCluster adds or updates a remote cluster connection.
// POST /api/v1/clusters
func (h *ClusterHandler) UpsertRemoteCluster(c *gin.Context) {
	ctx, span := clusterTracer.Start(c.Request.Context(), "ClusterHandler.UpsertRemoteCluster")
	defer span.End()

	var req UpsertRemoteClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	span.SetAttributes(attribute.String("cluster.frontend_address", req.FrontendAddress))
	h.logger.Info("upserting remote cluster",
		zap.String("frontend_address", req.FrontendAddress),
		zap.Bool("enable_connection", req.EnableConnection),
	)

	_, err := h.svc.AddOrUpdateRemoteCluster(ctx, &operatorservice.AddOrUpdateRemoteClusterRequest{
		FrontendAddress:               req.FrontendAddress,
		EnableRemoteClusterConnection: req.EnableConnection,
		FrontendHttpAddress:           req.FrontendHttpAddress,
		EnableReplication:             req.EnableReplication,
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("failed to upsert remote cluster",
			zap.String("frontend_address", req.FrontendAddress),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upsert remote cluster"})
		return
	}

	h.logger.Info("remote cluster upserted", zap.String("frontend_address", req.FrontendAddress))
	c.Status(http.StatusNoContent)
}
