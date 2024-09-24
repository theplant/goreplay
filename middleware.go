package relay

import (
	"context"

	"github.com/samber/lo"
)

// Middleware is a wrapper for ApplyCursorsFunc (middleware pattern)
type Middleware[T any] func(next ApplyCursorsFunc[T]) ApplyCursorsFunc[T]

// PaginationMiddleware is a wrapper for Pagination (middleware pattern)
type PaginationMiddleware[T any] func(next Pagination[T]) Pagination[T]

type ctxMiddlewares struct{}

func MiddlewaresFromContext[T any](ctx context.Context) []Middleware[T] {
	middlewares, _ := ctx.Value(ctxMiddlewares{}).([]Middleware[T])
	return middlewares
}

func AppendMiddleware[T any](ws ...Middleware[T]) PaginationMiddleware[T] {
	return func(next Pagination[T]) Pagination[T] {
		return PaginationFunc[T](func(ctx context.Context, req *PaginateRequest[T]) (*PaginateResponse[T], error) {
			middlewares := MiddlewaresFromContext[T](ctx)
			middlewares = append(middlewares, ws...)
			ctx = context.WithValue(ctx, ctxMiddlewares{}, middlewares)
			return next.Paginate(ctx, req)
		})
	}
}

func PrimaryOrderBys[T any](primaryOrderBys ...OrderBy) Middleware[T] {
	return func(next ApplyCursorsFunc[T]) ApplyCursorsFunc[T] {
		return func(ctx context.Context, req *ApplyCursorsRequest) (*ApplyCursorsResponse[T], error) {
			orderByFields := lo.SliceToMap(req.OrderBys, func(orderBy OrderBy) (string, bool) {
				return orderBy.Field, true
			})
			// If there are fields in primaryOrderBys that are not in orderBys, add them to orderBys
			for _, primaryOrderBy := range primaryOrderBys {
				if _, ok := orderByFields[primaryOrderBy.Field]; !ok {
					req.OrderBys = append(req.OrderBys, primaryOrderBy)
				}
			}
			return next(ctx, req)
		}
	}
}
