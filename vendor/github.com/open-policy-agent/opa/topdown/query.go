package topdown

import (
	"context"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

// QueryResultSet represents a collection of results returned by a query.
type QueryResultSet []QueryResult

// QueryResult represents a single result returned by a query. The result
// contains bindings for all variables that appear in the query.
type QueryResult map[ast.Var]*ast.Term

// Query provides a configurable interface for performing query evaluation.
type Query struct {
	cancel   Cancel
	query    ast.Body
	compiler *ast.Compiler
	store    storage.Store
	txn      storage.Transaction
	input    *ast.Term
	tracer   Tracer
	partial  []*ast.Term
	metrics  metrics.Metrics
}

// NewQuery returns a new Query object that can be run.
func NewQuery(query ast.Body) *Query {
	return &Query{query: query}
}

// WithCompiler sets the compiler to use for the query.
func (q *Query) WithCompiler(compiler *ast.Compiler) *Query {
	q.compiler = compiler
	return q
}

// WithStore sets the store to use for the query.
func (q *Query) WithStore(store storage.Store) *Query {
	q.store = store
	return q
}

// WithTransaction sets the transaction to use for the query. All queries
// should be performed over a consistent snapshot of the storage layer.
func (q *Query) WithTransaction(txn storage.Transaction) *Query {
	q.txn = txn
	return q
}

// WithCancel sets the cancellation object to use for the query. Set this if
// you need to abort queries based on a deadline. This is optional.
func (q *Query) WithCancel(cancel Cancel) *Query {
	q.cancel = cancel
	return q
}

// WithInput sets the input object to use for the query. References rooted at
// input will be evaluated against this value. This is optional.
func (q *Query) WithInput(input *ast.Term) *Query {
	q.input = input
	return q
}

// WithTracer sets the query tracer to use during evaluation. This is optional.
func (q *Query) WithTracer(tracer Tracer) *Query {
	q.tracer = tracer
	return q
}

// WithMetrics sets the metrics collection to add evaluation metrics to. This
// is optional.
func (q *Query) WithMetrics(metrics metrics.Metrics) *Query {
	q.metrics = metrics
	return q
}

// WithPartial sets the initial set of vars or refs to treat as unavailable
// during query evaluation. This is typically required for partial evaluation.
func (q *Query) WithPartial(terms []*ast.Term) *Query {
	q.partial = terms
	return q
}

// PartialRun is a wrapper around PartialIter that accumulates results and returns
// them in one shot.
func (q *Query) PartialRun(ctx context.Context) ([]ast.Body, error) {
	partials := []ast.Body{}
	return partials, q.PartialIter(ctx, func(partial ast.Body) error {
		partials = append(partials, partial)
		return nil
	})
}

// PartialIter executes the query invokes the iter function with partially
// evaluated queries produced by evaluating the query with a partial set.
func (q *Query) PartialIter(ctx context.Context, iter func(ast.Body) error) error {
	e := &eval{
		ctx:          ctx,
		cancel:       q.cancel,
		query:        q.query,
		bindings:     newBindings(),
		compiler:     q.compiler,
		store:        q.store,
		txn:          q.txn,
		input:        q.input,
		tracer:       q.tracer,
		builtinCache: builtins.Cache{},
		virtualCache: newVirtualCache(),
		saveSet:      newSaveSet(q.partial),
		saveStack:    newSaveStack(),
	}
	q.startTimer()
	defer q.stopTimer()
	return e.Run(func(e *eval) error {
		body := ast.NewBody()
		for _, elem := range e.saveStack.Stack {
			body.Append(plugExpr(elem.Bindings, elem.Expr))
		}
		e.bindings.Iter(func(a, b *ast.Term) error {
			body.Append(ast.Equality.Expr(a, b))
			return nil
		})
		return iter(body)
	})
}

// Run is a wrapper around Iter that accumulates query results and returns them
// in one shot.
func (q *Query) Run(ctx context.Context) (QueryResultSet, error) {
	qrs := QueryResultSet{}
	return qrs, q.Iter(ctx, func(qr QueryResult) error {
		qrs = append(qrs, qr)
		return nil
	})
}

// Iter executes the query and invokes the iter function with query results
// produced by evaluating the query.
func (q *Query) Iter(ctx context.Context, iter func(QueryResult) error) error {
	e := &eval{
		ctx:          ctx,
		cancel:       q.cancel,
		query:        q.query,
		bindings:     newBindings(),
		compiler:     q.compiler,
		store:        q.store,
		txn:          q.txn,
		input:        q.input,
		tracer:       q.tracer,
		builtinCache: builtins.Cache{},
		virtualCache: newVirtualCache(),
	}
	q.startTimer()
	defer q.stopTimer()
	return e.Run(func(e *eval) error {
		qr := QueryResult{}
		e.bindings.Iter(func(k, v *ast.Term) error {
			qr[k.Value.(ast.Var)] = v
			return nil
		})
		return iter(qr)
	})
}

func (q *Query) startTimer() {
	if q.metrics != nil {
		q.metrics.Timer(metrics.RegoQueryEval).Start()
	}
}

func (q *Query) stopTimer() {
	if q.metrics != nil {
		q.metrics.Timer(metrics.RegoQueryEval).Stop()
	}
}

func plugExpr(b *bindings, expr *ast.Expr) *ast.Expr {
	expr = expr.Copy()
	switch terms := expr.Terms.(type) {
	case *ast.Term:
		expr.Terms = b.Plug(terms)
	case []*ast.Term:
		for i := range terms {
			terms[i] = b.Plug(terms[i])
		}
	}
	return expr
}
