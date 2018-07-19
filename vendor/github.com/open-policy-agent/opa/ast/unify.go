// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

// Unify returns a set of variables that will be unified when the equality expression defined by
// terms a and b is evaluated. The unifier assumes that variables in the VarSet safe are already
// unified.
func Unify(safe VarSet, a *Term, b *Term) VarSet {
	u := &unifier{
		safe:    safe,
		unified: VarSet{},
		unknown: map[Var]VarSet{},
	}
	u.unify(a, b)
	return u.unified
}

type unifier struct {
	safe    VarSet
	unified VarSet
	unknown map[Var]VarSet
}

func (u *unifier) isSafe(x Var) bool {
	return u.safe.Contains(x) || u.unified.Contains(x)
}

func (u *unifier) unify(a *Term, b *Term) {

	switch a := a.Value.(type) {

	case Var:
		switch b := b.Value.(type) {
		case Var:
			if u.isSafe(b) {
				u.markSafe(a)
			} else if u.isSafe(a) {
				u.markSafe(b)
			} else {
				u.markUnknown(a, b)
				u.markUnknown(b, a)
			}
		case Array, Object:
			u.unifyAll(a, b)
		case Ref:
			if u.isSafe(b[0].Value.(Var)) {
				u.markSafe(a)
			}
		default:
			u.markSafe(a)
		}

	case Ref:
		if u.isSafe(a[0].Value.(Var)) {
			switch b := b.Value.(type) {
			case Var:
				u.markSafe(b)
			case Array, Object:
				u.markAllSafe(b)
			}
		}

	case *ArrayComprehension:
		switch b := b.Value.(type) {
		case Var:
			u.markSafe(b)
		case Array:
			u.markAllSafe(b)
		}
	case *ObjectComprehension:
		switch b := b.Value.(type) {
		case Var:
			u.markSafe(b)
		case Object:
			u.markAllSafe(b)
		}
	case *SetComprehension:
		switch b := b.Value.(type) {
		case Var:
			u.markSafe(b)
		}

	case Array:
		switch b := b.Value.(type) {
		case Var:
			u.unifyAll(b, a)
		case Ref, *ArrayComprehension, *ObjectComprehension, *SetComprehension:
			u.markAllSafe(a)
		case Array:
			if len(a) == len(b) {
				for i := range a {
					u.unify(a[i], b[i])
				}
			}
		}

	case Object:
		switch b := b.Value.(type) {
		case Var:
			u.unifyAll(b, a)
		case Ref:
			u.markAllSafe(a)
		case Object:
			if a.Len() == b.Len() {
				a.Iter(func(k, v *Term) error {
					if v2 := b.Get(k); v2 != nil {
						u.unify(v, v2)
					}
					return nil
				})
			}
		}

	default:
		switch b := b.Value.(type) {
		case Var:
			u.markSafe(b)
		}
	}
}

func (u *unifier) markAllSafe(x Value) {
	vis := u.varVisitor()
	Walk(vis, x)
	for v := range vis.Vars() {
		u.markSafe(v)
	}
}

func (u *unifier) markSafe(x Var) {
	u.unified.Add(x)

	// Add dependencies of 'x' to safe set
	vs := u.unknown[x]
	delete(u.unknown, x)
	for v := range vs {
		u.markSafe(v)
	}

	// Add dependants of 'x' to safe set if they have no more
	// dependencies.
	for v, deps := range u.unknown {
		if deps.Contains(x) {
			delete(deps, x)
			if len(deps) == 0 {
				u.markSafe(v)
			}
		}
	}
}

func (u *unifier) markUnknown(a, b Var) {
	if _, ok := u.unknown[a]; !ok {
		u.unknown[a] = NewVarSet()
	}
	u.unknown[a].Add(b)
}

func (u *unifier) unifyAll(a Var, b Value) {
	if u.isSafe(a) {
		u.markAllSafe(b)
	} else {
		vis := u.varVisitor()
		Walk(vis, b)
		unsafe := vis.Vars().Diff(u.safe).Diff(u.unified)
		if len(unsafe) == 0 {
			u.markSafe(a)
		} else {
			for v := range unsafe {
				u.markUnknown(a, v)
			}
		}
	}
}

func (u *unifier) varVisitor() *VarVisitor {
	return NewVarVisitor().WithParams(VarVisitorParams{
		SkipRefHead:    true,
		SkipObjectKeys: true,
		SkipClosures:   true,
	})
}
