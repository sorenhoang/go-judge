package main

import (
	"context"
	"log"
	"net"

	"github.com/sorenhoang/go-judge/internal/judge"
	coderunnerpb "github.com/sorenhoang/go-judge/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	coderunnerpb.UnimplementedCodeRunnerServiceServer
	runner judge.Runner
}

func (s server) RunTests(ctx context.Context, req *coderunnerpb.RunTestsRequest) (*coderunnerpb.RunTestsResponse, error) {
	result, err := s.runner.Run(ctx, req.Code, req.TestCode)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to run tests: %v", err)
	}

	return &coderunnerpb.RunTestsResponse{
		Verdict:     string(result.Verdict),
		Output:      result.Output,
		TotalTests:  int32(result.TotalTests),
		PassedTests: int32(result.PassedTests),
	}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	coderunnerpb.RegisterCodeRunnerServiceServer(grpcServer, &server{
		runner: judge.NewRunner(),
	})

	log.Println("gRPC server is running on port 50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
