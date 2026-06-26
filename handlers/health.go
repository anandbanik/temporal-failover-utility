package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

var healthTracer = otel.Tracer("temporal-utility/handlers")

type clusterInfoClient interface {
	GetClusterInfo(ctx context.Context, req *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error)
}

type clusterInfoWrapper struct {
	ws workflowservice.WorkflowServiceClient
}

func (w *clusterInfoWrapper) GetClusterInfo(ctx context.Context, req *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error) {
	return w.ws.GetClusterInfo(ctx, req)
}

type HealthHandler struct {
	svc    clusterInfoClient
	logger *zap.Logger
}

func NewHealthHandler(c client.Client, logger *zap.Logger) *HealthHandler {
	return newHealthHandler(&clusterInfoWrapper{ws: c.WorkflowService()}, logger)
}

func newHealthHandler(svc clusterInfoClient, logger *zap.Logger) *HealthHandler {
	return &HealthHandler{svc: svc, logger: logger}
}

type TemporalHealthResponse struct {
	Status        string `json:"status"`
	ClusterName   string `json:"cluster_name,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
}

// CheckTemporalHealth godoc
//
//	@Summary      Check Temporal server health
//	@Description  Verifies connectivity to the Temporal server by calling GetClusterInfo.
//	@Tags         health
//	@Produce      json
//	@Success      200  {object}  TemporalHealthResponse
//	@Failure      503  {object}  TemporalHealthResponse
//	@Router       /api/v1/health/temporal [get]
func (h *HealthHandler) CheckTemporalHealth(c *gin.Context) {
	ctx, span := healthTracer.Start(c.Request.Context(), "HealthHandler.CheckTemporalHealth")
	defer span.End()

	resp, err := h.svc.GetClusterInfo(ctx, &workflowservice.GetClusterInfoRequest{})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		h.logger.Error("temporal server health check failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, TemporalHealthResponse{Status: "unavailable"})
		return
	}

	c.JSON(http.StatusOK, TemporalHealthResponse{
		Status:        "ok",
		ClusterName:   resp.GetClusterName(),
		ServerVersion: resp.GetServerVersion(),
	})
}
