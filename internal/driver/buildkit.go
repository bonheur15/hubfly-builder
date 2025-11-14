package driver

import (
	"context"
	"io"

	"github.com/moby/buildkit/client"
)

type BuildKit struct {
	client *client.Client
}

func NewBuildKit(host string) (*BuildKit, error) {
	c, err := client.New(context.Background(), host)
	if err != nil {
		return nil, err
	}
	return &BuildKit{client: c}, nil
}

func (bk *BuildKit) Build(ctx context.Context, w io.Writer, opts client.SolveOpt) (*client.SolveResponse, error) {
	return bk.client.Solve(ctx, nil, opts, nil)
}
