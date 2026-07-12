package runnerclient

import (
	"context"

	"github.com/sorenhoang/go-judge/internal/submission"
	coderunnerpb "github.com/sorenhoang/go-judge/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Result struct {
	Verdict     submission.Status
	Output      string
	TotalTests  int
	PassedTests int
}

type Client struct {
	grpcClient coderunnerpb.CodeRunnerServiceClient
}

func Dial(target string) (*Client, func() error, error) {
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}

	return &Client{
		grpcClient: coderunnerpb.NewCodeRunnerServiceClient(conn),
	}, conn.Close, nil
}

func (c *Client) Run(ctx context.Context, code string, testCode string) (Result, error) {
	resp, err := c.grpcClient.RunTests(ctx, &coderunnerpb.RunTestsRequest{
		Code:     code,
		TestCode: testCode,
	})

	if err != nil {
		return Result{}, err
	}

	return Result{
		Verdict:     submission.Status(resp.Verdict),
		Output:      resp.Output,
		TotalTests:  int(resp.TotalTests),
		PassedTests: int(resp.PassedTests),
	}, nil
}
