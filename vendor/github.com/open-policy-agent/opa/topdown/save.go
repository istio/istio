package topdown

import (
	"github.com/open-policy-agent/opa/ast"
)

// saveSet contains a set of terms that are treated as unknown. The set is
// implemented as a tree so that we can partially evaluate data/input that is
// only partially known. E.g., policies expressed over input.x and input.y may
// be partially evaluated assuming input.x is known and input.y is unknown.
//
// Any time a variable is unified with an element in the set (an unknown), the
// variable is added to the set so that subsequent references to the variable
// are also treated as unknown.
type saveSet struct {
	s []*saveSetElem
}

func newSaveSet(terms []*ast.Term) *saveSet {
	return &saveSet{[]*saveSetElem{newSaveSetElem(terms)}}
}

func (n *saveSet) Empty() bool {
	if n != nil {
		for i := range n.s {
			if len(n.s[i].children) != 0 {
				return false
			}
		}
	}
	return true
}

func (n *saveSet) Contains(x *ast.Term) bool {
	if n != nil {
		for i := len(n.s) - 1; i >= 0; i-- {
			if n.s[i].Contains(x) {
				return true
			}
		}
	}
	return false
}

func (n *saveSet) ContainsRecursive(x *ast.Term) bool {
	stop := false
	ast.WalkTerms(x, func(t *ast.Term) bool {
		if stop {
			return true
		}
		if n.Contains(t) {
			stop = true
		}
		return stop
	})
	return stop
}

func (n *saveSet) ContainsRecursiveAny(xs []*ast.Term) bool {
	for i := range xs {
		if n.ContainsRecursive(xs[i]) {
			return true
		}
	}
	return false
}

func (n *saveSet) Push(x *saveSetElem) {
	n.s = append(n.s, x)
}

func (n *saveSet) Pop() {
	n.s = n.s[:len(n.s)-1]
}

func (n *saveSet) Vars() ast.VarSet {
	if n == nil {
		return nil
	}
	result := ast.NewVarSet()
	for i := 0; i < len(n.s); i++ {
		for k := range n.s[i].children {
			if v, ok := k.(ast.Var); ok {
				result.Add(v)
			}
		}
	}
	return result
}

type saveSetElem struct {
	children map[ast.Value]*saveSetElem
}

func newSaveSetElem(terms []*ast.Term) *saveSetElem {
	elem := &saveSetElem{
		children: map[ast.Value]*saveSetElem{},
	}
	for i := range terms {
		elem.Insert(terms[i])
	}
	return elem
}

func (n *saveSetElem) Empty() bool {
	return n == nil || len(n.children) == 0
}

func (n *saveSetElem) Contains(x *ast.Term) bool {
	switch x := x.Value.(type) {
	case ast.Ref:
		curr := n
		for i := 0; i < len(x); i++ {
			if curr = curr.child(x[i].Value); curr == nil {
				return false
			} else if curr.Empty() {
				return true
			}
		}
		return true
	case ast.Var, ast.String:
		return n.child(x) != nil
	default:
		return false
	}
}

func (n *saveSetElem) Insert(x *ast.Term) *saveSetElem {
	if n == nil {
		return nil
	}
	switch v := x.Value.(type) {
	case ast.Ref:
		curr := n
		for i := 0; i < len(v); i++ {
			curr = curr.Insert(v[i])
		}
		return curr
	case ast.Var, ast.String:
		child := n.children[v]
		if child == nil {
			child = newSaveSetElem(nil)
			n.children[v] = child
		}
		return child
	}
	return nil
}

func (n *saveSetElem) child(v ast.Value) *saveSetElem {
	if n == nil {
		return nil
	}
	return n.children[v]
}

// saveStack contains a stack of queries that represent the result of partial
// evaluation. When partial evaluation completes, the top of the stack
// represents a complete, partially evaluated query that can be saved and
// evaluated later.
//
// The result is stored in a stack so that partial evaluation of a query can be
// paused and then resumed in cases where different queries make up the result
// of partial evaluation, such as when a rule with a default clause is
// partially evaluated. In this case, the partially evaluated rule will be
// output in the support module.
type saveStack struct {
	Stack []saveStackQuery
}

func newSaveStack() *saveStack {
	return &saveStack{
		Stack: []saveStackQuery{
			{},
		},
	}
}

func (s *saveStack) PushQuery(query saveStackQuery) {
	s.Stack = append(s.Stack, query)
}

func (s *saveStack) PopQuery() saveStackQuery {
	last := s.Stack[len(s.Stack)-1]
	s.Stack = s.Stack[:len(s.Stack)-1]
	return last
}

func (s *saveStack) Push(expr *ast.Expr, b1 *bindings, b2 *bindings) {
	idx := len(s.Stack) - 1
	s.Stack[idx] = append(s.Stack[idx], saveStackElem{expr, b1, b2})
}

func (s *saveStack) Pop() {
	idx := len(s.Stack) - 1
	query := s.Stack[idx]
	s.Stack[idx] = query[:len(query)-1]
}

type saveStackQuery []saveStackElem

func (s saveStackQuery) Plug(b *bindings) ast.Body {
	if len(s) == 0 {
		return ast.NewBody(ast.NewExpr(ast.BooleanTerm(true)))
	}
	result := make(ast.Body, len(s))
	for i := range s {
		expr := s[i].Plug(b)
		result.Set(expr, i)
	}
	return result
}

type saveStackElem struct {
	Expr *ast.Expr
	B1   *bindings
	B2   *bindings
}

func (e saveStackElem) Plug(caller *bindings) *ast.Expr {
	expr := e.Expr.Copy()
	switch terms := expr.Terms.(type) {
	case []*ast.Term:
		if expr.IsEquality() {
			terms[1] = e.B1.PlugNamespaced(terms[1], caller)
			terms[2] = e.B2.PlugNamespaced(terms[2], caller)
		} else {
			for i := 1; i < len(terms); i++ {
				terms[i] = e.B1.PlugNamespaced(terms[i], caller)
			}
		}
	case *ast.Term:
		expr.Terms = e.B1.PlugNamespaced(terms, caller)
	}
	for i := range expr.With {
		expr.With[i].Value = e.B1.PlugNamespaced(expr.With[i].Value, caller)
	}
	return expr
}

// saveSupport contains additional partially evaluated policies that are part
// of the output of partial evaluation.
//
// The support structure is accumulated as partial evaluation runs and then
// considered complete once partial evaluation finishes (but not before). This
// differs from partially evaluated queries which are considered complete as
// soon as each one finishes.
type saveSupport struct {
	modules map[string]*ast.Module
}

func newSaveSupport() *saveSupport {
	return &saveSupport{
		modules: map[string]*ast.Module{},
	}
}

func (s *saveSupport) List() []*ast.Module {
	result := []*ast.Module{}
	for _, module := range s.modules {
		result = append(result, module)
	}
	return result
}

func (s *saveSupport) Exists(path ast.Ref) bool {
	k := path[:len(path)-1].String()
	module, ok := s.modules[k]
	if !ok {
		return false
	}
	name := ast.Var(path[len(path)-1].Value.(ast.String))
	for _, rule := range module.Rules {
		if rule.Head.Name.Equal(name) {
			return true
		}
	}
	return false
}

func (s *saveSupport) Insert(path ast.Ref, rule *ast.Rule) {
	pkg := path[:len(path)-1]
	k := pkg.String()
	module, ok := s.modules[k]
	if !ok {
		module = &ast.Module{
			Package: &ast.Package{
				Path: pkg,
			},
		}
		s.modules[k] = module
	}
	rule.Module = module
	module.Rules = append(module.Rules, rule)
}
