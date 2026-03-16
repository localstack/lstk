package emulator

import "context"

type Resource struct {
	Service string
	Name    string
	Region  string
	Account string
}

type Client interface {
	FetchVersion(ctx context.Context, host string) (string, error)
	FetchResources(ctx context.Context, host string) ([]Resource, error)
}
