package topdown

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/topdown/builtins"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
)

type evalIterator func(*eval) error

type unifyIterator func() error

type eval struct {
	ctx          context.Context
	queryID      uint64
	parent       *eval
	cancel       Cancel
	query        ast.Body
	bindings     *bindings
	store        storage.Store
	txn          storage.Transaction
	compiler     *ast.Compiler
	input        *ast.Term
	tracer       Tracer
	builtinCache builtins.Cache
	virtualCache *virtualCache
	saveSet      *saveSet
	saveStack    *saveStack
}

func (e *eval) Run(iter evalIterator) error {
	e.traceEnter(e.query)
	return e.eval(func(e *eval) error {
		e.traceExit(e.query)
		err := iter(e)
		e.traceRedo(e.query)
		return err
	})
}

func (e *eval) closure(query ast.Body) *eval {
	cpy := *e
	cpy.query = query
	cpy.queryID++
	cpy.parent = e
	return &cpy
}

func (e *eval) child(query ast.Body) *eval {
	cpy := *e
	cpy.bindings = newBindings()
	cpy.query = query
	cpy.queryID++
	cpy.parent = e
	return &cpy
}

func (e *eval) traceEnter(x interface{}) {
	e.traceEvent(EnterOp, x)
}

func (e *eval) traceExit(x interface{}) {
	e.traceEvent(ExitOp, x)
}

func (e *eval) traceEval(x interface{}) {
	e.traceEvent(EvalOp, x)
}

func (e *eval) traceFail(x interface{}) {
	e.traceEvent(FailOp, x)
}

func (e *eval) traceRedo(x interface{}) {
	e.traceEvent(RedoOp, x)
}

func (e *eval) traceEvent(op Op, x interface{}) {

	if e.tracer == nil || !e.tracer.Enabled() {
		return
	}

	locals := ast.NewValueMap()
	e.bindings.Iter(func(k, v *ast.Term) error {
		locals.Put(k.Value, v.Value)
		return nil
	})

	var parentID uint64
	if e.parent != nil {
		parentID = e.parent.queryID
	}

	evt := &Event{
		QueryID:  e.queryID,
		ParentID: parentID,
		Op:       op,
		Node:     x,
		Locals:   locals,
	}

	e.tracer.Trace(evt)
}

func (e *eval) traceEnabled() bool {
	return e.tracer != nil && e.tracer.Enabled()
}

func (e *eval) eval(iter evalIterator) error {
	return e.evalExpr(0, iter)
}

func (e *eval) evalExpr(index int, iter evalIterator) error {

	if e.cancel != nil && e.cancel.Cancelled() {
		return &Error{
			Code:    CancelErr,
			Message: "caller cancelled query execution",
		}
	}

	if index >= len(e.query) {
		return iter(e)
	}

	expr := e.query[index]
	e.traceEval(expr)

	if len(expr.With) > 0 {
		return e.evalWith(index, iter)
	}

	return e.evalStep(index, iter)
}

func (e *eval) evalStep(index int, iter evalIterator) error {

	expr := e.query[index]

	if expr.Negated {
		return e.evalNot(index, iter)
	}

	var defined bool
	var err error

	switch terms := expr.Terms.(type) {
	case []*ast.Term:
		if expr.IsEquality() {
			err = e.unify(terms[1], terms[2], func() error {
				defined = true
				err := e.evalExpr(index+1, iter)
				e.traceRedo(expr)
				return err
			})
		} else {
			err = e.evalCall(index, terms[0], terms[1:], func() error {
				defined = true
				err := e.evalExpr(index+1, iter)
				e.traceRedo(expr)
				return err
			})
		}
	case *ast.Term:
		rterm := ast.VarTerm(ast.WildcardPrefix + "term" + fmt.Sprintf("%d_%d", e.queryID, index))
		err = e.unify(terms, rterm, func() error {
			if !e.saveSet.Contains(rterm) {
				if !e.bindings.Plug(rterm).Equal(ast.BooleanTerm(false)) {
					defined = true
					err := e.evalExpr(index+1, iter)
					e.traceRedo(expr)
					return err
				}
				return nil
			}
			return e.saveCall(ast.NewTerm(ast.NotEqual.Ref()), []*ast.Term{rterm, ast.BooleanTerm(false)}, ast.BooleanTerm(true), func() error {
				return e.evalExpr(index+1, iter)
			})
		})
	}

	if err != nil {
		return err
	}

	if !defined {
		e.traceFail(expr)
	}

	return nil
}

func (e *eval) evalNot(index int, iter evalIterator) error {

	expr := e.query[index]
	negation := expr.Complement().NoWith()
	child := e.closure(ast.NewBody(negation))

	var defined bool

	err := child.eval(func(*eval) error {
		defined = true
		return nil
	})

	if err != nil {
		return err
	}

	if !defined {
		return e.evalExpr(index+1, iter)
	}

	e.traceFail(expr)
	return nil
}

func (e *eval) evalWith(index int, iter evalIterator) error {

	expr := e.query[index]
	pairs := make([][2]*ast.Term, len(expr.With))

	for i := range expr.With {
		plugged := e.bindings.Plug(expr.With[i].Value)
		pairs[i] = [...]*ast.Term{expr.With[i].Target, plugged}
	}

	input, err := makeInput(pairs)
	if err != nil {
		return &Error{
			Code:     ConflictErr,
			Location: expr.Location,
			Message:  err.Error(),
		}
	}

	old := e.input
	e.input = ast.NewTerm(input)
	e.virtualCache.Push()
	err = e.evalStep(index, iter)
	e.virtualCache.Pop()
	e.input = old
	return err
}

func (e *eval) evalCall(index int, operator *ast.Term, terms []*ast.Term, iter unifyIterator) error {

	ref := operator.Value.(ast.Ref)

	if ref[0].Equal(ast.DefaultRootDocument) {
		eval := evalFunc{
			e:     e,
			ref:   ref,
			terms: terms,
		}
		return eval.eval(iter)
	}

	bi := ast.BuiltinMap[ref.String()]
	if bi == nil {
		return unsupportedBuiltinErr(e.query[index].Location)
	}

	f := builtinFunctions[bi.Name]
	if f == nil {
		return unsupportedBuiltinErr(e.query[index].Location)
	}

	bctx := BuiltinContext{
		Cache:    e.builtinCache,
		Location: e.query[index].Location,
	}

	eval := evalBuiltin{
		e:     e,
		bi:    bi,
		bctx:  bctx,
		f:     f,
		terms: terms,
	}
	return eval.eval(iter)
}

func (e *eval) unify(a, b *ast.Term, iter unifyIterator) error {
	return e.biunify(a, b, e.bindings, e.bindings, iter)
}

func (e *eval) biunify(a, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {
	a, b1 = b1.apply(a)
	b, b2 = b2.apply(b)
	switch vA := a.Value.(type) {
	case ast.Var, ast.Ref, *ast.ArrayComprehension, *ast.SetComprehension, *ast.ObjectComprehension:
		return e.biunifyValues(a, b, b1, b2, iter)
	case ast.Null:
		switch b.Value.(type) {
		case ast.Var, ast.Null, ast.Ref:
			return e.biunifyValues(a, b, b1, b2, iter)
		}
	case ast.Boolean:
		switch b.Value.(type) {
		case ast.Var, ast.Boolean, ast.Ref:
			return e.biunifyValues(a, b, b1, b2, iter)
		}
	case ast.Number:
		switch b.Value.(type) {
		case ast.Var, ast.Number, ast.Ref:
			return e.biunifyValues(a, b, b1, b2, iter)
		}
	case ast.String:
		switch b.Value.(type) {
		case ast.Var, ast.String, ast.Ref:
			return e.biunifyValues(a, b, b1, b2, iter)
		}
	case ast.Array:
		switch vB := b.Value.(type) {
		case ast.Var, ast.Ref, *ast.ArrayComprehension:
			return e.biunifyValues(a, b, b1, b2, iter)
		case ast.Array:
			return e.biunifyArrays(vA, vB, b1, b2, iter)
		}
	case ast.Object:
		switch vB := b.Value.(type) {
		case ast.Var, ast.Ref, *ast.ObjectComprehension:
			return e.biunifyValues(a, b, b1, b2, iter)
		case ast.Object:
			return e.biunifyObjects(vA, vB, b1, b2, iter)
		}
	case *ast.Set:
		return e.biunifyValues(a, b, b1, b2, iter)
	}
	return nil
}

func (e *eval) biunifyArrays(a, b ast.Array, b1, b2 *bindings, iter unifyIterator) error {
	if len(a) != len(b) {
		return nil
	}
	return e.biunifyArraysRec(a, b, b1, b2, iter, 0)
}

func (e *eval) biunifyArraysRec(a, b ast.Array, b1, b2 *bindings, iter unifyIterator, idx int) error {
	if idx == len(a) {
		return iter()
	}
	return e.biunify(a[idx], b[idx], b1, b2, func() error {
		return e.biunifyArraysRec(a, b, b1, b2, iter, idx+1)
	})
}

func (e *eval) biunifyObjects(a, b ast.Object, b1, b2 *bindings, iter unifyIterator) error {
	if len(a) != len(b) {
		return nil
	}
	return e.biunifyObjectsRec(a, b, b1, b2, iter, 0)
}

func (e *eval) biunifyObjectsRec(a, b ast.Object, b1, b2 *bindings, iter unifyIterator, idx int) error {
	if idx == len(a) {
		return iter()
	}
	item := a[idx]
	other := b.Get(item[0])
	if other == nil {
		return nil
	}
	return e.biunify(item[1], other, b1, b2, func() error {
		return e.biunifyObjectsRec(a, b, b1, b2, iter, idx+1)
	})
}

func (e *eval) biunifyValues(a, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {

	saveA := e.saveSet.Contains(a)
	saveB := e.saveSet.Contains(b)

	if saveA && saveB {
		return e.saveUnify(a, b, b1, b2, iter)
	}

	_, refA := a.Value.(ast.Ref)
	_, refB := b.Value.(ast.Ref)

	if refA && !saveA {
		return e.biunifyRef(a, b, b1, b2, iter)
	} else if refA && saveA {
		return e.saveUnify(a, b, b1, b2, iter)
	} else if refB && !saveB {
		return e.biunifyRef(b, a, b2, b1, iter)
	} else if refB && saveB {
		return e.saveUnify(a, b, b1, b2, iter)
	}

	compA := ast.IsComprehension(a.Value)
	compB := ast.IsComprehension(b.Value)

	if saveA || saveB || ((compA || compB) && e.saveSet != nil) {
		return e.saveUnify(a, b, b1, b2, iter)
	}

	if compA {
		return e.biunifyComprehension(a.Value, b, b1, b2, iter)
	} else if compB {
		return e.biunifyComprehension(b.Value, a, b2, b1, iter)
	}

	_, varA := a.Value.(ast.Var)
	_, varB := b.Value.(ast.Var)

	if varA && varB {
		if b1 == b2 && a.Equal(b) {
			return iter()
		}
		undo := b1.bind(a, b, b2)
		err := iter()
		undo.Undo()
		return err
	} else if varA && !varB {
		undo := b1.bind(a, b, b2)
		err := iter()
		undo.Undo()
		return err
	} else if varB && !varA {
		undo := b2.bind(b, a, b1)
		err := iter()
		undo.Undo()
		return err
	}

	if a.Equal(b) {
		return iter()
	}

	return nil
}

func (e *eval) biunifyRef(a, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {

	ref := a.Value.(ast.Ref)
	plugged := make(ast.Ref, len(ref))
	plugged[0] = ref[0]

	if ref[0].Equal(ast.DefaultRootDocument) {
		node := e.compiler.RuleTree.Child(ref[0].Value)
		eval := evalTree{
			e:         e,
			ref:       ref,
			pos:       1,
			plugged:   plugged,
			bindings:  b1,
			rterm:     b,
			rbindings: b2,
			node:      node,
		}
		return eval.eval(iter)
	}

	var term *ast.Term

	if ref[0].Equal(ast.InputRootDocument) {
		term = e.input
	} else {
		term = b1.Plug(ref[0])
		if term == ref[0] {
			term = nil
		}
	}

	if term == nil {
		return nil
	}

	eval := evalTerm{
		e:            e,
		ref:          ref,
		pos:          1,
		bindings:     b1,
		term:         term,
		termbindings: b1,
		rterm:        b,
		rbindings:    b2,
	}

	return eval.eval(iter)
}

func (e *eval) biunifyComprehension(a interface{}, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {
	switch a := a.(type) {
	case *ast.ArrayComprehension:
		return e.biunifyComprehensionArray(a, b, b1, b2, iter)
	case *ast.SetComprehension:
		return e.biunifyComprehensionSet(a, b, b1, b2, iter)
	case *ast.ObjectComprehension:
		return e.biunifyComprehensionObject(a, b, b1, b2, iter)
	}
	return fmt.Errorf("illegal comprehension %T", a)
}

func (e *eval) biunifyComprehensionArray(x *ast.ArrayComprehension, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {
	result := ast.Array{}
	child := e.closure(x.Body)
	err := child.Run(func(child *eval) error {
		result = append(result, child.bindings.Plug(x.Term))
		return nil
	})
	if err != nil {
		return err
	}
	return e.biunify(ast.NewTerm(result), b, b1, b2, iter)
}

func (e *eval) biunifyComprehensionSet(x *ast.SetComprehension, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {
	result := &ast.Set{}
	child := e.closure(x.Body)
	err := child.Run(func(child *eval) error {
		result.Add(child.bindings.Plug(x.Term))
		return nil
	})
	if err != nil {
		return err
	}
	return e.biunify(ast.NewTerm(result), b, b1, b2, iter)
}

func (e *eval) biunifyComprehensionObject(x *ast.ObjectComprehension, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {
	result := ast.Object{}
	child := e.closure(x.Body)
	err := child.Run(func(child *eval) error {
		key := child.bindings.Plug(x.Key)
		value := child.bindings.Plug(x.Value)
		exist := result.Get(key)
		if exist != nil && !exist.Equal(value) {
			return objectDocKeyConflictErr(x.Key.Location)
		}
		result = append(result, ast.Item(key, value))
		return nil
	})
	if err != nil {
		return err
	}
	return e.biunify(ast.NewTerm(result), b, b1, b2, iter)
}

func (e *eval) saveUnify(a, b *ast.Term, b1, b2 *bindings, iter unifyIterator) error {
	expr := ast.Equality.Expr(a, b)
	elem := newSaveSetElem(e.getUnifyOutputs(expr))
	e.saveSet.Push(elem)
	defer e.saveSet.Pop()
	e.saveStack.Push(expr, b1)
	defer e.saveStack.Pop()
	return iter()
}

func (e *eval) saveCall(operator *ast.Term, args []*ast.Term, result *ast.Term, iter unifyIterator) error {
	terms := make([]*ast.Term, len(args)+1)
	terms[0] = operator
	for i := 1; i < len(terms); i++ {
		terms[i] = args[i-1]
	}
	expr := ast.NewExpr(terms)
	elem := newSaveSetElem([]*ast.Term{result})
	e.saveSet.Push(elem)
	defer e.saveSet.Pop()
	e.saveStack.Push(expr, e.bindings)
	defer e.saveStack.Pop()
	return iter()
}

func (e *eval) getRules(ref ast.Ref) (*ast.IndexResult, error) {
	if e.saveSet != nil {
		return e.getAllRules(ref), nil
	}
	return e.getRulesIndexed(ref)
}

func (e *eval) getAllRules(ref ast.Ref) *ast.IndexResult {
	var ir *ast.IndexResult
	for _, rule := range e.compiler.GetRulesExact(ref) {
		if ir == nil {
			ir = ast.NewIndexResult(rule.Head.DocKind())
		}
		if rule.Default {
			ir.Default = rule
		} else {
			ir.Rules = append(ir.Rules, rule)
			for els := rule.Else; els != nil; els = rule.Else {
				ir.Else[rule] = append(ir.Else[rule], els)
			}
		}
	}
	return ir
}

func (e *eval) getRulesIndexed(ref ast.Ref) (*ast.IndexResult, error) {
	index := e.compiler.RuleIndex(ref)
	if index == nil {
		return nil, nil
	}
	return index.Lookup(e)
}

func (e *eval) Resolve(ref ast.Ref) (ast.Value, error) {

	if ref[0].Equal(ast.InputRootDocument) {
		if e.input != nil {
			v, err := e.input.Value.Find(ref[1:])
			if err != nil {
				return nil, nil
			}
			return v, nil
		}
		return nil, nil
	}

	if ref[0].Equal(ast.DefaultRootDocument) {
		path, err := storage.NewPathForRef(ref)
		if err != nil {
			if !storage.IsNotFound(err) {
				return nil, err
			}
			return nil, nil
		}

		blob, err := e.store.Read(e.ctx, e.txn, path)
		if err != nil {
			if !storage.IsNotFound(err) {
				return nil, err
			}
			return nil, nil
		}

		if len(path) == 0 {
			obj := blob.(map[string]interface{})
			if len(obj) > 0 {
				cpy := make(map[string]interface{}, len(obj)-1)
				for k, v := range obj {
					if string(ast.SystemDocumentKey) == k {
						continue
					}
					cpy[k] = v
				}
				blob = cpy
			}
		}

		return ast.InterfaceToValue(blob)
	}

	return nil, fmt.Errorf("illegal ref")
}

func (e *eval) getUnifyOutputs(expr *ast.Expr) []*ast.Term {
	vars := expr.Vars(ast.VarVisitorParams{
		SkipClosures: true,
		SkipRefHead:  true,
	})
	outputs := make([]*ast.Term, 0, len(vars))
	for v := range vars {
		term := e.bindings.Plug(ast.NewTerm(v))
		if !term.IsGround() {
			outputs = append(outputs, term)
		}
	}
	return outputs
}

type evalBuiltin struct {
	e     *eval
	bi    *ast.Builtin
	bctx  BuiltinContext
	f     BuiltinFunc
	terms []*ast.Term
}

func (e evalBuiltin) eval(iter unifyIterator) error {

	nargs := len(e.bi.Decl.Args())
	nterms := len(e.terms)

	args := make([]*ast.Term, nargs)

	for i := 0; i < nargs; i++ {
		args[i] = e.e.bindings.Plug(e.terms[i])
	}

	return e.f(e.bctx, args, func(output *ast.Term) error {
		if nargs == nterms {
			return iter()
		}
		return e.e.unify(e.terms[nterms-1], output, iter)
	})
}

type evalFunc struct {
	e     *eval
	ref   ast.Ref
	terms []*ast.Term
}

func (e evalFunc) eval(iter unifyIterator) error {

	ir, err := e.e.getRules(e.ref)
	if err != nil {
		return err
	}

	if ir.Empty() {
		return nil
	}

	var prev *ast.Term

	for i := range ir.Rules {
		next, err := e.evalOneRule(iter, ir.Rules[i], prev)
		if err != nil {
			return err
		}
		if next == nil {
			for _, rule := range ir.Else[ir.Rules[i]] {
				next, err = e.evalOneRule(iter, rule, prev)
				if err != nil {
					return err
				}
				if next != nil {
					break
				}
			}
		}
		if next != nil {
			prev = next
		}
	}

	return nil
}

func (e evalFunc) evalOneRule(iter unifyIterator, rule *ast.Rule, prev *ast.Term) (*ast.Term, error) {

	child := e.e.child(rule.Body)

	b := make(ast.Array, len(e.terms))
	for i := range rule.Head.Args {
		b[i] = rule.Head.Args[i]
	}

	if len(b) == len(rule.Head.Args)+1 {
		b[len(b)-1] = rule.Head.Value
	}

	var result *ast.Term

	child.traceEnter(rule)

	err := child.biunifyArrays(e.terms, b, e.e.bindings, child.bindings, func() error {
		return child.eval(func(child *eval) error {
			child.traceExit(rule)
			result = child.bindings.Plug(rule.Head.Value)

			if prev != nil {
				if ast.Compare(prev, result) != 0 {
					return functionConflictErr(rule.Location)
				}
				child.traceRedo(rule)
				return nil
			}

			prev = result

			if err := iter(); err != nil {
				return err
			}

			child.traceRedo(rule)
			return nil
		})
	})

	return result, err
}

type evalTree struct {
	e         *eval
	ref       ast.Ref
	plugged   ast.Ref
	pos       int
	bindings  *bindings
	rterm     *ast.Term
	rbindings *bindings
	node      *ast.TreeNode
}

func (e evalTree) eval(iter unifyIterator) error {

	if len(e.ref) == e.pos {
		return e.finish(iter)
	}

	plugged := e.bindings.Plug(e.ref[e.pos])

	if plugged.IsGround() {
		return e.next(iter, plugged)
	}

	return e.enumerate(iter)
}

func (e evalTree) finish(iter unifyIterator) error {

	v, err := e.extent()
	if err != nil || v == nil {
		return err
	}

	return e.e.biunify(e.rterm, v, e.rbindings, e.bindings, func() error {
		return iter()
	})
}

func (e evalTree) next(iter unifyIterator, plugged *ast.Term) error {

	var node *ast.TreeNode

	if e.node != nil {
		node = e.node.Child(plugged.Value)
		if node != nil && len(node.Values) > 0 {
			r := evalVirtual{
				e:         e.e,
				ref:       e.ref,
				plugged:   e.plugged,
				pos:       e.pos,
				bindings:  e.bindings,
				rterm:     e.rterm,
				rbindings: e.rbindings,
			}
			r.plugged[e.pos] = plugged
			return r.eval(iter)
		}
	}

	cpy := e
	cpy.plugged[e.pos] = plugged
	cpy.node = node
	cpy.pos++
	return cpy.eval(iter)
}

func (e evalTree) enumerate(iter unifyIterator) error {

	doc, err := e.e.Resolve(e.plugged[:e.pos])
	if err != nil {
		return err
	}

	if doc != nil {
		switch doc := doc.(type) {
		case ast.Array:
			for i := range doc {
				k := ast.IntNumberTerm(i)
				err := e.e.biunify(k, e.ref[e.pos], e.bindings, e.bindings, func() error {
					return e.next(iter, k)
				})
				if err != nil {
					return err
				}
			}
		case ast.Object:
			for _, pair := range doc {
				err := e.e.biunify(pair[0], e.ref[e.pos], e.bindings, e.bindings, func() error {
					return e.next(iter, pair[0])
				})
				if err != nil {
					return err
				}
			}
		}
	}

	if e.node == nil {
		return nil
	}

	for k := range e.node.Children {
		key := ast.NewTerm(k)
		e.e.biunify(key, e.ref[e.pos], e.bindings, e.bindings, func() error {
			return e.next(iter, key)
		})
	}

	return nil
}

func (e evalTree) extent() (*ast.Term, error) {

	base, err := e.e.Resolve(e.plugged)
	if err != nil {
		return nil, err
	}

	virtual, err := e.leaves(e.plugged, e.node)
	if err != nil {
		return nil, err
	}

	if virtual == nil {
		if base == nil {
			return nil, nil
		}
		return ast.NewTerm(base), nil
	}

	if base != nil {
		merged, _ := base.(ast.Object).Merge(virtual)
		return ast.NewTerm(merged), nil
	}

	return ast.NewTerm(virtual), nil
}

func (e evalTree) leaves(plugged ast.Ref, node *ast.TreeNode) (ast.Object, error) {

	if e.node == nil {
		return nil, nil
	}

	result := ast.Object{}

	for _, child := range node.Children {
		if child.Hide {
			continue
		}

		plugged = append(plugged, ast.NewTerm(child.Key))

		var save ast.Value
		var err error

		if len(child.Values) > 0 {
			rterm := ast.VarTerm(ast.WildcardPrefix + "leaf")
			err = e.e.unify(ast.NewTerm(plugged), rterm, func() error {
				save = e.e.bindings.Plug(rterm).Value
				return nil
			})
		} else {
			save, err = e.leaves(plugged, child)
		}

		if err != nil {
			return nil, err
		}

		if save != nil {
			result, _ = result.Merge(ast.Object{ast.Item(plugged[len(plugged)-1], ast.NewTerm(save))})
		}

		plugged = plugged[:len(plugged)-1]
	}

	return result, nil
}

type evalVirtual struct {
	e         *eval
	ref       ast.Ref
	plugged   ast.Ref
	pos       int
	bindings  *bindings
	rterm     *ast.Term
	rbindings *bindings
}

func (e evalVirtual) eval(iter unifyIterator) error {

	ir, err := e.e.getRules(e.plugged[:e.pos+1])
	if err != nil {
		return err
	}

	switch ir.Kind {
	case ast.PartialSetDoc:
		eval := evalVirtualPartial{
			e:         e.e,
			ref:       e.ref,
			plugged:   e.plugged,
			pos:       e.pos,
			ir:        ir,
			bindings:  e.bindings,
			rterm:     e.rterm,
			rbindings: e.rbindings,
			empty:     ast.SetTerm(),
		}
		return eval.eval(iter)
	case ast.PartialObjectDoc:
		eval := evalVirtualPartial{
			e:         e.e,
			ref:       e.ref,
			plugged:   e.plugged,
			pos:       e.pos,
			ir:        ir,
			bindings:  e.bindings,
			rterm:     e.rterm,
			rbindings: e.rbindings,
			empty:     ast.ObjectTerm(),
		}
		return eval.eval(iter)
	default:
		eval := evalVirtualComplete{
			e:         e.e,
			ref:       e.ref,
			plugged:   e.plugged,
			pos:       e.pos,
			ir:        ir,
			bindings:  e.bindings,
			rterm:     e.rterm,
			rbindings: e.rbindings,
		}
		return eval.eval(iter)
	}
}

type evalVirtualPartial struct {
	e         *eval
	ref       ast.Ref
	plugged   ast.Ref
	pos       int
	ir        *ast.IndexResult
	bindings  *bindings
	rterm     *ast.Term
	rbindings *bindings
	empty     *ast.Term
}

func (e evalVirtualPartial) eval(iter unifyIterator) error {

	if len(e.ref) == e.pos+1 {
		return e.evalAllRules(iter, e.ir.Rules)
	}

	var cacheKey ast.Ref

	if e.ir.Kind == ast.PartialObjectDoc {
		plugged := e.bindings.Plug(e.ref[e.pos+1])

		if plugged.IsGround() {
			path := e.plugged[:e.pos+2]
			path[len(path)-1] = plugged
			cached := e.e.virtualCache.Get(path)

			if cached != nil {
				return e.evalTerm(iter, cached, e.bindings)
			}

			cacheKey = path
		}
	}

	for _, rule := range e.ir.Rules {
		if err := e.evalOneRule(iter, rule, cacheKey); err != nil {
			return err
		}
	}

	return nil
}

func (e evalVirtualPartial) evalAllRules(iter unifyIterator, rules []*ast.Rule) error {

	result := e.empty

	for _, rule := range rules {
		child := e.e.child(rule.Body)
		child.traceEnter(rule)

		err := child.eval(func(*eval) error {
			child.traceExit(rule)
			var err error
			result, err = e.reduce(rule.Head, child.bindings, result)
			if err != nil {
				return err
			}

			child.traceRedo(rule)
			return nil
		})

		if err != nil {
			return err
		}
	}

	return e.e.biunify(result, e.rterm, e.bindings, e.bindings, iter)
}

func (e evalVirtualPartial) evalOneRule(iter unifyIterator, rule *ast.Rule, cacheKey ast.Ref) error {

	key := e.ref[e.pos+1]
	child := e.e.child(rule.Body)

	child.traceEnter(rule)
	var defined bool

	err := child.biunify(rule.Head.Key, key, child.bindings, e.bindings, func() error {
		defined = true
		return child.eval(func(child *eval) error {
			child.traceExit(rule)

			term := rule.Head.Value
			if term == nil {
				term = rule.Head.Key
			}

			if cacheKey != nil {
				result := child.bindings.Plug(term)
				e.e.virtualCache.Put(cacheKey, result)
			}

			term, termbindings := child.bindings.apply(term)

			err := e.evalTerm(iter, term, termbindings)
			if err != nil {
				return err
			}

			child.traceRedo(rule)
			return nil
		})
	})

	if err != nil {
		return err
	}

	if !defined {
		child.traceFail(rule)
	}

	return nil
}

func (e evalVirtualPartial) evalTerm(iter unifyIterator, term *ast.Term, termbindings *bindings) error {
	eval := evalTerm{
		e:            e.e,
		ref:          e.ref,
		pos:          e.pos + 2,
		bindings:     e.bindings,
		term:         term,
		termbindings: termbindings,
		rterm:        e.rterm,
		rbindings:    e.rbindings,
	}
	return eval.eval(iter)
}

func (e evalVirtualPartial) reduce(head *ast.Head, b *bindings, result *ast.Term) (*ast.Term, error) {

	switch v := result.Value.(type) {
	case *ast.Set:
		v.Add(b.Plug(head.Key))
	case ast.Object:
		key := b.Plug(head.Key)
		value := b.Plug(head.Value)
		exist := v.Get(key)
		if exist != nil && !exist.Equal(value) {
			return nil, objectDocKeyConflictErr(head.Location)
		}
		v = append(v, ast.Item(key, value))
		result.Value = v
	}

	return result, nil
}

type evalVirtualComplete struct {
	e         *eval
	ref       ast.Ref
	plugged   ast.Ref
	pos       int
	ir        *ast.IndexResult
	bindings  *bindings
	rterm     *ast.Term
	rbindings *bindings
}

func (e evalVirtualComplete) eval(iter unifyIterator) error {

	if e.ir.Empty() {
		return nil
	}

	if len(e.ir.Rules) > 0 && len(e.ir.Rules[0].Head.Args) > 0 {
		return nil
	}

	cached := e.e.virtualCache.Get(e.plugged[:e.pos+1])

	if cached != nil {
		return e.evalTerm(iter, cached, e.bindings)
	}

	var prev *ast.Term

	for i := range e.ir.Rules {
		next, err := e.evalRule(iter, e.ir.Rules[i], prev)
		if err != nil {
			return err
		}
		if next == nil {
			for _, rule := range e.ir.Else[e.ir.Rules[i]] {
				next, err = e.evalRule(iter, rule, prev)
				if err != nil {
					return err
				}
				if next != nil {
					break
				}
			}
		}
		if next != nil {
			prev = next
		}
	}

	if e.ir.Default != nil && prev == nil {
		_, err := e.evalRule(iter, e.ir.Default, prev)
		return err
	}

	return nil
}

func (e evalVirtualComplete) evalRule(iter unifyIterator, rule *ast.Rule, prev *ast.Term) (*ast.Term, error) {

	child := e.e.child(rule.Body)
	var result *ast.Term

	child.traceEnter(rule)

	err := child.eval(func(child *eval) error {
		child.traceExit(rule)
		result = child.bindings.Plug(rule.Head.Value)

		if prev != nil {
			if ast.Compare(result, prev) != 0 {
				return completeDocConflictErr(rule.Location)
			}
			child.traceRedo(rule)
			return nil
		}

		e.e.virtualCache.Put(e.plugged[:e.pos+1], result)

		prev = result
		term, termbindings := child.bindings.apply(rule.Head.Value)
		err := e.evalTerm(iter, term, termbindings)
		if err != nil {
			return err
		}

		child.traceRedo(rule)
		return nil
	})

	if err != nil {
		return nil, err
	} else if result != nil {
		return result, nil
	}

	return nil, nil
}

func (e evalVirtualComplete) evalTerm(iter unifyIterator, term *ast.Term, termbindings *bindings) error {
	eval := evalTerm{
		e:            e.e,
		ref:          e.ref,
		pos:          e.pos + 1,
		bindings:     e.bindings,
		term:         term,
		termbindings: termbindings,
		rterm:        e.rterm,
		rbindings:    e.rbindings,
	}
	return eval.eval(iter)
}

type evalTerm struct {
	e            *eval
	ref          ast.Ref
	plugged      ast.Ref
	pos          int
	bindings     *bindings
	term         *ast.Term
	termbindings *bindings
	rterm        *ast.Term
	rbindings    *bindings
}

func (e evalTerm) eval(iter unifyIterator) error {

	if len(e.ref) == e.pos {
		return e.e.biunify(e.term, e.rterm, e.termbindings, e.rbindings, func() error {
			return iter()
		})
	}

	plugged := e.bindings.Plug(e.ref[e.pos])

	if plugged.IsGround() {
		return e.next(iter, plugged)
	}

	return e.enumerate(iter)
}

func (e evalTerm) next(iter unifyIterator, plugged *ast.Term) error {

	term, bindings := e.get(plugged)
	if term == nil {
		return nil
	}

	cpy := e
	cpy.term = term
	cpy.termbindings = bindings
	cpy.pos++
	return cpy.eval(iter)
}

func (e evalTerm) enumerate(iter unifyIterator) error {

	switch v := e.term.Value.(type) {
	case ast.Array:
		for i := range v {
			k := ast.IntNumberTerm(i)
			err := e.e.biunify(k, e.ref[e.pos], e.bindings, e.bindings, func() error {
				return e.next(iter, k)
			})
			if err != nil {
				return err
			}
		}
	case ast.Object:
		for _, pair := range v {
			err := e.e.biunify(pair[0], e.ref[e.pos], e.termbindings, e.bindings, func() error {
				return e.next(iter, e.bindings.Plug(e.ref[e.pos]))
			})
			if err != nil {
				return err
			}
		}
	case *ast.Set:
		for _, elem := range *v {
			err := e.e.biunify(elem, e.ref[e.pos], e.termbindings, e.bindings, func() error {
				return e.next(iter, e.bindings.Plug(e.ref[e.pos]))
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (e evalTerm) get(plugged *ast.Term) (*ast.Term, *bindings) {
	switch v := e.term.Value.(type) {
	case *ast.Set:
		if v.IsGround() {
			if v.Contains(plugged) {
				return e.termbindings.apply(plugged)
			}
		} else {
			for _, elem := range *v {
				if e.termbindings.Plug(elem).Equal(plugged) {
					return e.termbindings.apply(plugged)
				}
			}
		}
	case ast.Object:
		if v.IsGround() {
			term := v.Get(plugged)
			if term != nil {
				return e.termbindings.apply(term)
			}
		} else {
			for i := range v {
				if e.termbindings.Plug(v[i][0]).Equal(plugged) {
					return e.termbindings.apply(v[i][1])
				}
			}
		}
	case ast.Array:
		term := v.Get(plugged)
		if term != nil {
			return e.termbindings.apply(term)
		}
	}
	return nil, nil
}
