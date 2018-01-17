package topdown

import "github.com/open-policy-agent/opa/ast"

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

func (n *saveSet) ContainsAny(xs []*ast.Term) bool {
	for i := range xs {
		if n.Contains(xs[i]) {
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

type saveStack struct {
	Stack []saveStackElem
}

func newSaveStack() *saveStack {
	return &saveStack{}
}

type saveStackElem struct {
	Expr     *ast.Expr
	Bindings *bindings
}

func (s *saveStack) Push(expr *ast.Expr, b *bindings) {
	s.Stack = append(s.Stack, saveStackElem{expr, b})
}

func (s *saveStack) Pop() {
	s.Stack = s.Stack[:len(s.Stack)-1]
}
