// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/util"
)

type undo struct {
	k    *ast.Term
	u    *bindings
	next *undo
}

func (u *undo) Undo() {
	if u == nil {
		// Allow call on zero value of Undo for ease-of-use.
		return
	}
	if u.u == nil {
		// Call on empty unifier undos a no-op unify operation.
		return
	}
	u.u.delete(u.k)
	u.next.Undo()
}

type bindings struct {
	id     uint64
	values *util.HashMap
	instr  *Instrumentation
}

func newBindings(id uint64, instr *Instrumentation) *bindings {

	eq := func(a, b util.T) bool {
		v1, ok1 := a.(*ast.Term)
		if ok1 {
			v2 := b.(*ast.Term)
			return v1.Equal(v2)
		}
		uv1 := a.(*value)
		uv2 := b.(*value)
		return uv1.equal(uv2)
	}

	hash := func(x util.T) int {
		v := x.(*ast.Term)
		return v.Hash()
	}

	values := util.NewHashMap(eq, hash)

	return &bindings{id, values, instr}
}

func (u *bindings) Iter(caller *bindings, iter func(*ast.Term, *ast.Term) error) error {

	var err error

	u.values.Iter(func(k, v util.T) bool {
		if err != nil {
			return true
		}
		term := k.(*ast.Term)
		err = iter(term, u.PlugNamespaced(term, caller))
		return false
	})

	return err
}

func (u *bindings) Plug(a *ast.Term) *ast.Term {
	return u.PlugNamespaced(a, nil)
}

func (u *bindings) PlugNamespaced(a *ast.Term, caller *bindings) *ast.Term {
	if u != nil {
		u.instr.startTimer(evalOpPlug)
		defer u.instr.stopTimer(evalOpPlug)
	}
	return u.plugNamespaced(a, caller)
}

func (u *bindings) plugNamespaced(a *ast.Term, caller *bindings) *ast.Term {
	switch v := a.Value.(type) {
	case ast.Var:
		b, next := u.apply(a)
		if a != b || u != next {
			return next.plugNamespaced(b, caller)
		}
		if caller != nil && caller != u {
			if name, ok := b.Value.(ast.Var); ok {
				return ast.NewTerm(ast.Var(string(name) + fmt.Sprint(u.id)))
			}
		}
		return b
	case ast.Array:
		cpy := *a
		arr := make(ast.Array, len(v))
		for i := 0; i < len(arr); i++ {
			arr[i] = u.plugNamespaced(v[i], caller)
		}
		cpy.Value = arr
		return &cpy
	case ast.Object:
		cpy := *a
		cpy.Value, _ = v.Map(func(k, v *ast.Term) (*ast.Term, *ast.Term, error) {
			return u.plugNamespaced(k, caller), u.plugNamespaced(v, caller), nil
		})
		return &cpy
	case ast.Set:
		cpy := *a
		cpy.Value, _ = v.Map(func(x *ast.Term) (*ast.Term, error) {
			return u.plugNamespaced(x, caller), nil
		})
		return &cpy
	case ast.Ref:
		cpy := *a
		ref := make(ast.Ref, len(v))
		ref[0] = v[0]
		for i := 1; i < len(ref); i++ {
			ref[i] = u.plugNamespaced(v[i], caller)
		}
		cpy.Value = ref
		return &cpy
	}
	return a
}

func (u *bindings) bind(a *ast.Term, b *ast.Term, other *bindings) *undo {
	// See note in apply about non-var terms.
	_, ok := a.Value.(ast.Var)
	if !ok {
		panic("illegal value")
	}
	u.values.Put(a, value{
		u: other,
		v: b,
	})
	return &undo{a, u, nil}
}

func (u *bindings) apply(a *ast.Term) (*ast.Term, *bindings) {
	// Early exit for non-var terms. Only vars are bound in the binding list,
	// so the lookup below will always fail for non-var terms. In some cases,
	// the lookup may be expensive as it has to hash the term (which for large
	// inputs can be costly.)
	_, ok := a.Value.(ast.Var)
	if !ok {
		return a, u
	}
	val, ok := u.get(a)
	if !ok {
		return a, u
	}
	return val.u.apply(val.v)
}

func (u *bindings) delete(v *ast.Term) {
	u.values.Delete(v)
}

func (u *bindings) get(v *ast.Term) (value, bool) {
	if u == nil {
		return value{}, false
	}
	r, ok := u.values.Get(v)
	if !ok {
		return value{}, false
	}
	return r.(value), true
}

func (u *bindings) String() string {
	if u == nil {
		return "()"
	}
	var buf []string
	u.values.Iter(func(a, b util.T) bool {
		buf = append(buf, fmt.Sprintf("%v: %v", a, b))
		return false
	})
	return fmt.Sprintf("({%v}, %v)", strings.Join(buf, ", "), u.id)
}

type value struct {
	u *bindings
	v *ast.Term
}

func (v value) String() string {
	return fmt.Sprintf("(%v, %d)", v.v, v.u.id)
}

func (v value) equal(other *value) bool {
	if v.u == other.u {
		return v.v.Equal(other.v)
	}
	return false
}
