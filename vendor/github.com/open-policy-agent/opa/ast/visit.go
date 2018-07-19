// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

// Visitor defines the interface for iterating AST elements. The Visit function
// can return a Visitor w which will be used to visit the children of the AST
// element v. If the Visit function returns nil, the children will not be
// visited.
type Visitor interface {
	Visit(v interface{}) (w Visitor)
}

// BeforeAndAfterVisitor wraps Visitor to provie hooks for being called before
// and after the AST has been visited.
type BeforeAndAfterVisitor interface {
	Visitor
	Before(x interface{})
	After(x interface{})
}

// Walk iterates the AST by calling the Visit function on the Visitor
// v for x before recursing.
func Walk(v Visitor, x interface{}) {
	wrapped, ok := v.(BeforeAndAfterVisitor)
	if !ok {
		wrapped = noopBeforeAndAfterVisitor{v}
	}
	WalkBeforeAndAfter(wrapped, x)
}

// WalkBeforeAndAfter iterates the AST by calling the Visit function on the
// Visitor v for x before recursing.
func WalkBeforeAndAfter(v BeforeAndAfterVisitor, x interface{}) {
	v.Before(x)
	defer v.After(x)
	w := v.Visit(x)
	if w == nil {
		return
	}
	switch x := x.(type) {
	case *Module:
		Walk(w, x.Package)
		for _, i := range x.Imports {
			Walk(w, i)
		}
		for _, r := range x.Rules {
			Walk(w, r)
		}
		for _, c := range x.Comments {
			Walk(w, c)
		}
	case *Package:
		Walk(w, x.Path)
	case *Import:
		Walk(w, x.Path)
		Walk(w, x.Alias)
	case *Rule:
		Walk(w, x.Head)
		Walk(w, x.Body)
		if x.Else != nil {
			Walk(w, x.Else)
		}
	case *Head:
		Walk(w, x.Name)
		Walk(w, x.Args)
		if x.Key != nil {
			Walk(w, x.Key)
		}
		if x.Value != nil {
			Walk(w, x.Value)
		}
	case Body:
		for _, e := range x {
			Walk(w, e)
		}
	case Args:
		for _, t := range x {
			Walk(w, t)
		}
	case *Expr:
		switch ts := x.Terms.(type) {
		case []*Term:
			for _, t := range ts {
				Walk(w, t)
			}
		case *Term:
			Walk(w, ts)
		}
		for i := range x.With {
			Walk(w, x.With[i])
		}
	case *With:
		Walk(w, x.Target)
		Walk(w, x.Value)
	case *Term:
		Walk(w, x.Value)
	case Ref:
		for _, t := range x {
			Walk(w, t)
		}
	case Object:
		x.Foreach(func(k, v *Term) {
			Walk(w, k)
			Walk(w, v)
		})
	case Array:
		for _, t := range x {
			Walk(w, t)
		}
	case Set:
		x.Foreach(func(t *Term) {
			Walk(w, t)
		})
	case *ArrayComprehension:
		Walk(w, x.Term)
		Walk(w, x.Body)
	case *ObjectComprehension:
		Walk(w, x.Key)
		Walk(w, x.Value)
		Walk(w, x.Body)
	case *SetComprehension:
		Walk(w, x.Term)
		Walk(w, x.Body)
	case Call:
		for _, t := range x {
			Walk(w, t)
		}
	}
}

// WalkVars calls the function f on all vars under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkVars(x interface{}, f func(Var) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if v, ok := x.(Var); ok {
			return f(v)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkClosures calls the function f on all closures under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkClosures(x interface{}, f func(interface{}) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		switch x.(type) {
		case *ArrayComprehension, *ObjectComprehension, *SetComprehension:
			return f(x)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkRefs calls the function f on all references under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkRefs(x interface{}, f func(Ref) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if r, ok := x.(Ref); ok {
			return f(r)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkTerms calls the function f on all terms under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkTerms(x interface{}, f func(*Term) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if term, ok := x.(*Term); ok {
			return f(term)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkWiths calls the function f on all with modifiers under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkWiths(x interface{}, f func(*With) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if w, ok := x.(*With); ok {
			return f(w)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkExprs calls the function f on all expressions under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkExprs(x interface{}, f func(*Expr) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if r, ok := x.(*Expr); ok {
			return f(r)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkBodies calls the function f on all bodies under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkBodies(x interface{}, f func(Body) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if b, ok := x.(Body); ok {
			return f(b)
		}
		return false
	}}
	Walk(vis, x)
}

// WalkRules calls the function f on all rules under x. If the function f
// returns true, AST nodes under the last node will not be visited.
func WalkRules(x interface{}, f func(*Rule) bool) {
	vis := &GenericVisitor{func(x interface{}) bool {
		if r, ok := x.(*Rule); ok {
			return f(r)
		}
		return false
	}}
	Walk(vis, x)
}

// GenericVisitor implements the Visitor interface to provide
// a utility to walk over AST nodes using a closure. If the closure
// returns true, the visitor will not walk over AST nodes under x.
type GenericVisitor struct {
	f func(x interface{}) bool
}

// NewGenericVisitor returns a new GenericVisitor that will invoke the function
// f on AST nodes.
func NewGenericVisitor(f func(x interface{}) bool) *GenericVisitor {
	return &GenericVisitor{f}
}

// Visit calls the function f on the GenericVisitor.
func (vis *GenericVisitor) Visit(x interface{}) Visitor {
	if vis.f(x) {
		return nil
	}
	return vis
}

// VarVisitor walks AST nodes under a given node and collects all encountered
// variables. The collected variables can be controlled by specifying
// VarVisitorParams when creating the visitor.
type VarVisitor struct {
	params VarVisitorParams
	vars   VarSet
}

// VarVisitorParams contains settings for a VarVisitor.
type VarVisitorParams struct {
	SkipRefHead     bool
	SkipRefCallHead bool
	SkipObjectKeys  bool
	SkipClosures    bool
	SkipWithTarget  bool
	SkipSets        bool
}

// NewVarVisitor returns a new VarVisitor object.
func NewVarVisitor() *VarVisitor {
	return &VarVisitor{
		vars: NewVarSet(),
	}
}

// WithParams sets the parameters in params on vis.
func (vis *VarVisitor) WithParams(params VarVisitorParams) *VarVisitor {
	vis.params = params
	return vis
}

// Vars returns a VarSet that contains collected vars.
func (vis *VarVisitor) Vars() VarSet {
	return vis.vars
}

// Visit is called to walk the AST node v.
func (vis *VarVisitor) Visit(v interface{}) Visitor {
	if vis.params.SkipObjectKeys {
		if o, ok := v.(Object); ok {
			o.Foreach(func(_, v *Term) {
				Walk(vis, v)
			})
			return nil
		}
	}
	if vis.params.SkipRefHead {
		if r, ok := v.(Ref); ok {
			for _, t := range r[1:] {
				Walk(vis, t)
			}
			return nil
		}
	}
	if vis.params.SkipClosures {
		switch v.(type) {
		case *ArrayComprehension, *ObjectComprehension, *SetComprehension:
			return nil
		}
	}
	if vis.params.SkipWithTarget {
		if v, ok := v.(*With); ok {
			Walk(vis, v.Value)
			return nil
		}
	}
	if vis.params.SkipSets {
		if _, ok := v.(Set); ok {
			return nil
		}
	}
	if vis.params.SkipRefCallHead {
		if expr, ok := v.(*Expr); ok {
			if terms, ok := expr.Terms.([]*Term); ok {
				for _, t := range terms[0].Value.(Ref)[1:] {
					Walk(vis, t)
				}
				for i := 1; i < len(terms); i++ {
					Walk(vis, terms[i])
				}
				for _, w := range expr.With {
					Walk(vis, w)
				}
				return nil
			}
		}
	}
	if v, ok := v.(Var); ok {
		vis.vars.Add(v)
	}
	return vis
}

type noopBeforeAndAfterVisitor struct {
	Visitor
}

func (noopBeforeAndAfterVisitor) Before(interface{}) {}
func (noopBeforeAndAfterVisitor) After(interface{})  {}
