// @title           Temporal Utility API
// @version         1.0
// @description     HTTP service for managing Temporal namespaces and cluster topology.
// @host            localhost:9090
// @BasePath        /

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	"go.uber.org/zap"

	_ "temporal-utility/docs"

	"temporal-utility/config"
	"temporal-utility/handlers"
	"temporal-utility/router"
	temporalclient "temporal-utility/temporal"
	"temporal-utility/telemetry"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	defer logger.Sync() //nolint:errcheck

	cfg := config.Load()
	ctx := context.Background()

	otelSDK, err := telemetry.Setup(ctx, &cfg.OTEL, logger)
	if err != nil {
		logger.Fatal("failed to setup OTEL", zap.Error(err))
	}

	temporalClient, err := temporalclient.NewClient(&cfg.Temporal, logger)
	if err != nil {
		logger.Fatal("failed to create Temporal client", zap.Error(err))
	}

	namespaceHandler := handlers.NewNamespaceHandler(temporalClient, logger)
	clusterHandler := handlers.NewClusterHandler(temporalClient, logger)
	handoverHandler := handlers.NewHandoverHandler(temporalClient, logger)
	r := router.New(namespaceHandler, clusterHandler, handoverHandler, logger)

	if os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		// Lambda mode: Temporal client and OTel SDK are reused across warm invocations.
		// The runtime manages process lifetime; no explicit cleanup is registered.
		logger.Info("starting in Lambda mode")
		ginLambda := ginadapter.New(r)
		lambda.Start(func(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
			return ginLambda.ProxyWithContext(ctx, req)
		})
		return
	}

	// Local HTTP server mode.
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server starting", zap.String("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	<-sigCtx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}
	if err := otelSDK.Shutdown(shutdownCtx); err != nil {
		logger.Error("OTEL shutdown error", zap.Error(err))
	}
	temporalClient.Close()

	logger.Info("shutdown complete")
}
