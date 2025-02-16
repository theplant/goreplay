package gormrelay

import (
	"context"
	"reflect"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/theplant/relay"
	"github.com/theplant/relay/cursor"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func buildKeysetExpr(s *schema.Schema, orderBys []relay.OrderBy, keyset map[string]any, reverse bool) (clause.Expression, error) {
	ors := make([]clause.Expression, 0, len(orderBys))
	eqs := make([]clause.Expression, 0, len(orderBys))
	for i, orderBy := range orderBys {
		v, ok := keyset[orderBy.Field]
		if !ok {
			return nil, errors.Errorf("missing field %q in keyset", orderBy.Field)
		}

		field, ok := s.FieldsByName[orderBy.Field]
		if !ok {
			return nil, errors.Errorf("missing field %q in schema", orderBy.Field)
		}

		desc := orderBy.Desc
		if reverse {
			desc = !desc
		}

		column := clause.Column{Table: clause.CurrentTable, Name: field.DBName}

		var expr clause.Expression
		if desc {
			expr = clause.Lt{Column: column, Value: v}
		} else {
			expr = clause.Gt{Column: column, Value: v}
		}

		ands := make([]clause.Expression, len(eqs)+1)
		copy(ands, eqs)
		ands[len(eqs)] = expr
		ors = append(ors, clause.And(ands...))

		if i < len(orderBys)-1 {
			eqs = append(eqs, clause.Eq{Column: column, Value: v})
		}
	}
	return clause.And(clause.Or(ors...)), nil
}

// Example:
// db.Clauses(
//
//	 	// This is for `Where`, so we cant use `Where(clause.And(clause.Or(...),clause.Or(...)))`
//		clause.And(
//			clause.Or( // after
//				clause.And(
//					clause.Gt{Column: "age", Value: 85}, // ASC
//				),
//				clause.And(
//					clause.Eq{Column: "age", Value: 85},
//					clause.Lt{Column: "name", Value: "name15"}, // DESC
//				),
//			),
//		),
//		clause.And(
//			clause.Or( // before
//				clause.And(
//					clause.Lt{Column: "age", Value: 88},
//				),
//				clause.And(
//					clause.Eq{Column: "age", Value: 88},
//					clause.Gt{Column: "name", Value: "name12"},
//				),
//			),
//		),
//		clause.OrderBy{
//			Columns: []clause.OrderByColumn{
//				{Column: clause.Column{Name: "age"}, Desc: false},
//				{Column: clause.Column{Name: "name"}, Desc: true},
//			},
//		},
//		clause.Limit{Limit: &limit},
//
// )
func scopeKeyset(after, before *map[string]any, orderBys []relay.OrderBy, limit int, fromEnd bool) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if db.Statement.Model == nil {
			db.AddError(errors.New("model is nil"))
			return db
		}

		s, err := parseSchema(db, db.Statement.Model)
		if err != nil {
			db.AddError(err)
			return db
		}

		var exprs []clause.Expression

		if after != nil {
			expr, err := buildKeysetExpr(s, orderBys, *after, false)
			if err != nil {
				db.AddError(err)
				return db
			}
			exprs = append(exprs, expr)
		}

		if before != nil {
			expr, err := buildKeysetExpr(s, orderBys, *before, true)
			if err != nil {
				db.AddError(err)
				return db
			}
			exprs = append(exprs, expr)
		}

		if len(orderBys) > 0 {
			orderByColumns := make([]clause.OrderByColumn, 0, len(orderBys))
			for _, orderBy := range orderBys {
				field, ok := s.FieldsByName[orderBy.Field]
				if !ok {
					db.AddError(errors.Errorf("missing field %q in schema", orderBy.Field))
					return db
				}

				desc := orderBy.Desc
				if fromEnd {
					desc = !desc
				}
				orderByColumns = append(orderByColumns, clause.OrderByColumn{
					Column: clause.Column{Table: clause.CurrentTable, Name: field.DBName},
					Desc:   desc,
				})
			}
			exprs = append(exprs, clause.OrderBy{Columns: orderByColumns})
		}

		if limit > 0 {
			exprs = append(exprs, clause.Limit{Limit: &limit})
		} else {
			db.AddError(errors.New("limit must be greater than 0"))
		}

		return db.Clauses(exprs...)
	}
}

func findByKeyset[T any](db *gorm.DB, after, before *map[string]any, orderBys []relay.OrderBy, limit int, fromEnd bool) ([]T, error) {
	var nodes []T
	if limit == 0 {
		return nodes, nil
	}

	basedOnModel, err := shouldBasedOnModel[T](db)
	if err != nil {
		return nil, err
	}

	if basedOnModel {
		modelType := reflect.TypeOf(db.Statement.Model)
		sliceType := reflect.SliceOf(modelType)
		nodesVal := reflect.New(sliceType).Elem()

		err := db.Scopes(scopeKeyset(after, before, orderBys, limit, fromEnd)).Find(nodesVal.Addr().Interface()).Error
		if err != nil {
			return nil, errors.Wrap(err, "find")
		}

		nodes := make([]T, nodesVal.Len())
		for i := 0; i < nodesVal.Len(); i++ {
			nodes[i] = nodesVal.Index(i).Interface().(T)
		}

		if fromEnd {
			lo.Reverse(nodes)
		}
		return nodes, nil
	}

	if db.Statement.Model == nil {
		var t T
		db = db.Model(t)
	}

	err = db.Scopes(scopeKeyset(after, before, orderBys, limit, fromEnd)).Find(&nodes).Error
	if err != nil {
		return nil, errors.Wrap(err, "find")
	}
	if fromEnd {
		lo.Reverse(nodes)
	}
	return nodes, nil
}

type KeysetFinder[T any] struct {
	db *gorm.DB
}

func NewKeysetFinder[T any](db *gorm.DB) *KeysetFinder[T] {
	return &KeysetFinder[T]{db: db}
}

func (a *KeysetFinder[T]) Find(ctx context.Context, after, before *map[string]any, orderBys []relay.OrderBy, limit int, fromEnd bool) ([]T, error) {
	if limit == 0 {
		return []T{}, nil
	}

	db := a.db
	if db.Statement.Context != ctx {
		db = db.WithContext(ctx)
	}

	nodes, err := findByKeyset[T](db, after, before, orderBys, limit, fromEnd)
	if err != nil {
		return nil, err
	}

	return nodes, nil
}

func (a *KeysetFinder[T]) Count(ctx context.Context) (int, error) {
	db := a.db

	basedOnModel, err := shouldBasedOnModel[T](db)
	if err != nil {
		return 0, err
	}

	if db.Statement.Context != ctx {
		db = db.WithContext(ctx)
	}

	if !basedOnModel && db.Statement.Model == nil {
		var t T
		db = db.Model(t)
	}

	var totalCount int64
	if err := db.Count(&totalCount).Error; err != nil {
		return 0, errors.Wrap(err, "count")
	}
	return int(totalCount), nil
}

func NewKeysetAdapter[T any](db *gorm.DB) relay.ApplyCursorsFunc[T] {
	return cursor.NewKeysetAdapter(NewKeysetFinder[T](db))
}
