package relay

import "context"

type Skip struct {
	Edges, Nodes, TotalCount, PageInfo bool
}

func (s Skip) All() bool {
	return s.Edges && s.Nodes && s.TotalCount && s.PageInfo
}

type ctxKeySkip struct{}

func WithSkip(ctx context.Context, skip Skip) context.Context {
	return context.WithValue(ctx, ctxKeySkip{}, skip)
}

func GetSkip(ctx context.Context) Skip {
	skip, _ := ctx.Value(ctxKeySkip{}).(Skip)
	return skip
}

type ctxKeyNodeProcessor struct{}

func WithNodeProcessor[T any](ctx context.Context, processor func(node T) T) context.Context {
	return context.WithValue(ctx, ctxKeyNodeProcessor{}, processor)
}

func GetNodeProcessor[T any](ctx context.Context) func(node T) T {
	processor, _ := ctx.Value(ctxKeyNodeProcessor{}).(func(node T) T)
	return processor
}
