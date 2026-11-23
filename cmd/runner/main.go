package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sorenhoang/go-judge/internal/judge"
	coderunnerpb "github.com/sorenhoang/go-judge/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testRunner interface {
	Run(ctx context.Context, code, testCode string) (judge.Result, error)
}

type server struct {
	coderunnerpb.UnimplementedCodeRunnerServiceServer
	runner testRunner
}

func (s server) RunTests(ctx context.Context, req *coderunnerpb.RunTestsRequest) (*coderunnerpb.RunTestsResponse, error) {
	result, err := s.runner.Run(ctx, req.Code, req.TestCode)

	switch {
	case err == nil:
	case errors.Is(err, judge.ErrSandboxUnavailable):
		return nil, status.Errorf(codes.Unavailable, "%v", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return nil, status.FromContextError(err).Err()
	default:
		return nil, status.Errorf(codes.Internal, "failed to run tests: %v", err)
	}

	return &coderunnerpb.RunTestsResponse{
		Verdict:     string(result.Verdict),
		Output:      result.Output,
		TotalTests:  int32(result.TotalTests),
		PassedTests: int32(result.PassedTests),
	}, nil
}

// shutdownGrace is how long in-flight runs may keep going after SIGTERM before
// they're cancelled. Longer than judge's sandbox timeout so a normal run
// finishes. A var so tests can shrink it.
var shutdownGrace = 35 * time.Second

// serve blocks until ctx is cancelled and every in-flight RPC has returned.
// Waiting matters: each RunTests cleans up its sandbox container on return, so
// exiting early would leave the container running.
func serve(ctx context.Context, lis net.Listener, runner testRunner) error {
	// WaitForHandlers: Stop (and so Serve) only returns once every cancelled
	// run's handler has returned, i.e. after its cleanup
	grpcServer := grpc.NewServer(grpc.WaitForHandlers(true))
	coderunnerpb.RegisterCodeRunnerServiceServer(grpcServer, &server{runner: runner})

	go func() {
		<-ctx.Done()
		log.Printf("shutting down, waiting up to %s for in-flight runs", shutdownGrace)
		hardStop := time.AfterFunc(shutdownGrace, grpcServer.Stop) // cancels the remaining runs
		grpcServer.GracefulStop()
		hardStop.Stop()
	}()

	return grpcServer.Serve(lis)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// once shutdown starts, a second Ctrl-C/SIGTERM kills the process right away
	context.AfterFunc(ctx, stop)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	log.Println("gRPC server is running on port 50051")
	if err := serve(ctx, lis, judge.NewRunner()); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	log.Println("runner shutdown complete")
}
