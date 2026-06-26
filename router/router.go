package router

import (
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	"temporal-utility/handlers"
	"temporal-utility/middleware"
)

func New(namespaceHandler *handlers.NamespaceHandler, clusterHandler *handlers.ClusterHandler, handoverHandler *handlers.HandoverHandler, healthHandler *handlers.HealthHandler, logger *zap.Logger) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.OTEL())
	r.Use(middleware.Logger(logger))

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1")
	{
		v1.POST("/namespaces", namespaceHandler.CreateNamespace)
		v1.POST("/namespaces/:name/promote", namespaceHandler.PromoteNamespace)
		v1.POST("/namespaces/:name/handover", handoverHandler.Handover)
		v1.POST("/clusters", clusterHandler.UpsertRemoteCluster)
		v1.GET("/health/temporal", healthHandler.CheckTemporalHealth)
	}

	return r
}
