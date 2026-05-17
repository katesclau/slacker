package openaiadapter

import (
	"context"
	"strings"
)

type modelKey struct{}

func WithModel(ctx context.Context, name string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, modelKey{}, strings.TrimSpace(name))
}

func modelFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(modelKey{}).(string)
	if !ok {
		return ""
	}
	return v
}
