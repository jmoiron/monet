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

func WithPageOverride(ctx context.Context, page int) context.Context {
	return sources.WithPageOverride(ctx, page)
}

func PageOverrideFromContext(ctx context.Context) int {
	return sources.PageOverrideFromContext(ctx)
}
