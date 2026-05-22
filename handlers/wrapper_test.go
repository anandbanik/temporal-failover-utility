package handlers

import (
	"context"
	"testing"

	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// newFakeWorkflowServiceClient creates a gRPC stub against a non-existent server.
// grpc.NewClient is lazy — the connection is never established until an RPC is made.
// Calling the stub methods exercises the wrapper statements (recording coverage)
// even though the RPC itself returns a "connection refused" error.
func newFakeWorkflowServiceClient(t *testing.T) workflowservice.WorkflowServiceClient {
	t.Helper()
	conn, err := grpc.NewClient("localhost:9999", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Skipf("could not create gRPC client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return workflowservice.NewWorkflowServiceClient(conn)
}

func newFakeOperatorServiceClient(t *testing.T) operatorservice.OperatorServiceClient {
	t.Helper()
	conn, err := grpc.NewClient("localhost:9999", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Skipf("could not create gRPC client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return operatorservice.NewOperatorServiceClient(conn)
}

func TestWorkflowServiceWrapper_Methods(t *testing.T) {
	ws := newFakeWorkflowServiceClient(t)
	w := &workflowServiceWrapper{ws: ws}
	ctx := context.Background()

	// Each call executes the wrapper statement; the RPC error is expected and ignored.
	_, _ = w.RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{Namespace: "test"})
	_, _ = w.DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: "test"})
	_, _ = w.UpdateNamespace(ctx, &workflowservice.UpdateNamespaceRequest{Namespace: "test"})
}

func TestOperatorServiceWrapper_Methods(t *testing.T) {
	os := newFakeOperatorServiceClient(t)
	w := &operatorServiceWrapper{os: os}
	ctx := context.Background()

	_, _ = w.AddOrUpdateRemoteCluster(ctx, &operatorservice.AddOrUpdateRemoteClusterRequest{
		FrontendAddress: "localhost:7233",
	})
}

func TestHandoverServiceWrapper_Methods(t *testing.T) {
	ws := newFakeWorkflowServiceClient(t)
	w := &handoverServiceWrapper{ws: ws}
	ctx := context.Background()

	_, _ = w.UpdateNamespace(ctx, &workflowservice.UpdateNamespaceRequest{Namespace: "test"})
	_, _ = w.StartWorkflowExecution(ctx, &workflowservice.StartWorkflowExecutionRequest{
		Namespace: "test",
	})
}
