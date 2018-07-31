package topdown

import (
	"context"
	"sort"

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
	cancel           Cancel
	query            ast.Body
	compiler         *ast.Compiler
	store            storage.Store
	txn              storage.Transaction
	input            *ast.Term
	tracer           Tracer
	unknowns         []*ast.Term
	partialNamespace string
	metrics          metrics.Metrics
	instr            *Instrumentation
	genvarprefix     string
}

// NewQuery returns a new Query object that can be run.
func NewQuery(query ast.Body) *Query {
	return &Query{
		query:        query,
		genvarprefix: ast.WildcardPrefix,
	}
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
func (q *Query) WithMetrics(m metrics.Metrics) *Query {
	q.metrics = m
	return q
}

// WithInstrumentation sets the instrumentation configuration to enable on the
// evaluation process. By default, instrumentation is turned off.
func (q *Query) WithInstrumentation(instr *Instrumentation) *Query {
	q.instr = instr
	return q
}

// WithUnknowns sets the initial set of variables or references to treat as
// unknown during query evaluation. This is required for partial evaluation.
func (q *Query) WithUnknowns(terms []*ast.Term) *Query {
	q.unknowns = terms
	return q
}

// WithPartialNamespace sets the namespace to use for supporting rules
// generated as part of the partial evaluation process. The ns value must be a
// valid package path component.
func (q *Query) WithPartialNamespace(ns string) *Query {
	q.partialNamespace = ns
	return q
}

// PartialRun executes partial evaluation on the query with respect to unknown
// values. Partial evaluation attempts to evaluate as much of the query as
// possible without requiring values for the unknowns set on the query. The
// result of partial evaluation is a new set of queries that can be evaluated
// once the unknown value is known. In addition to new queries, partial
// evaluation may produce additional support modules that should be used in
// conjunction with the partially evaluated queries.
func (q *Query) PartialRun(ctx context.Context) (partials []ast.Body, support []*ast.Module, err error) {
	if q.partialNamespace == "" {
		q.partialNamespace = "partial" // lazily initialize partial namespace
	}
	f := &queryIDFactory{}
	e := &eval{
		ctx:           ctx,
		cancel:        q.cancel,
		query:         q.query,
		queryIDFact:   f,
		queryID:       f.Next(),
		bindings:      newBindings(0, q.instr),
		compiler:      q.compiler,
		store:         q.store,
		baseCache:     newBaseCache(),
		txn:           q.txn,
		input:         q.input,
		tracer:        q.tracer,
		instr:         q.instr,
		builtinCache:  builtins.Cache{},
		virtualCache:  newVirtualCache(),
		saveSet:       newSaveSet(q.unknowns),
		saveStack:     newSaveStack(),
		saveSupport:   newSaveSupport(),
		saveNamespace: ast.StringTerm(q.partialNamespace),
		genvarprefix:  q.genvarprefix,
	}
	q.startTimer(metrics.RegoPartialEval)
	defer q.stopTimer(metrics.RegoPartialEval)
	err = e.Run(func(e *eval) error {
		// Build output from saved expressions.
		body := ast.NewBody()
		for _, elem := range e.saveStack.Stack[len(e.saveStack.Stack)-1] {
			body.Append(elem.Plug(e.bindings))
		}
		// Include bindings as exprs so that when caller evals the result, they
		// can obtain values for the vars in their query.
		bindingExprs := []*ast.Expr{}
		e.bindings.Iter(e.bindings, func(a, b *ast.Term) error {
			bindingExprs = append(bindingExprs, ast.Equality.Expr(a, b))
			return nil
		})
		// Sort binding expressions so that results are deterministic.
		sort.Slice(bindingExprs, func(i, j int) bool {
			return bindingExprs[i].Compare(bindingExprs[j]) < 0
		})
		for i := range bindingExprs {
			body.Append(bindingExprs[i])
		}
		partials = append(partials, body)
		return nil
	})
	return partials, e.saveSupport.List(), err
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
	f := &queryIDFactory{}
	e := &eval{
		ctx:          ctx,
		cancel:       q.cancel,
		query:        q.query,
		queryIDFact:  f,
		queryID:      f.Next(),
		bindings:     newBindings(0, q.instr),
		compiler:     q.compiler,
		store:        q.store,
		baseCache:    newBaseCache(),
		txn:          q.txn,
		input:        q.input,
		tracer:       q.tracer,
		instr:        q.instr,
		builtinCache: builtins.Cache{},
		virtualCache: newVirtualCache(),
		genvarprefix: q.genvarprefix,
	}
	q.startTimer(metrics.RegoQueryEval)
	defer q.stopTimer(metrics.RegoQueryEval)
	return e.Run(func(e *eval) error {
		qr := QueryResult{}
		e.bindings.Iter(nil, func(k, v *ast.Term) error {
			qr[k.Value.(ast.Var)] = v
			return nil
		})
		return iter(qr)
	})
}

func (q *Query) startTimer(name string) {
	if q.metrics != nil {
		q.metrics.Timer(name).Start()
	}
}

func (q *Query) stopTimer(name string) {
	if q.metrics != nil {
		q.metrics.Timer(name).Stop()
	}
}
