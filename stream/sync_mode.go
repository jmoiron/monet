package stream

import (
	"context"

	"github.com/jmoiron/monet/stream/sources"
)

type SyncMode = sources.SyncMode

const (
	SyncModeIncremental = sources.SyncModeIncremental
	SyncModeFull        = sources.SyncModeFull
)

func WithSyncMode(ctx context.Context, mode SyncMode) context.Context {
	return sources.WithSyncMode(ctx, mode)
}

func SyncModeFromContext(ctx context.Context) SyncMode {
	return sources.SyncModeFromContext(ctx)
}

func WithRepoBackfill(ctx context.Context, repo string) context.Context {
	return sources.WithRepoBackfill(ctx, repo)
}

func RepoBackfillFromContext(ctx context.Context) string {
	return sources.RepoBackfillFromContext(ctx)
}
