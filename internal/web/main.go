package web

import (
	"context"
)

// Main constructs the default web server and runs it until ctx is cancelled.
// It is the library entrypoint used by cmd/openfsd when -web is enabled.
func Main(ctx context.Context) error {
	server, err := NewDefaultServer(ctx)
	if err != nil {
		return err
	}
	return server.Run(ctx)
}
