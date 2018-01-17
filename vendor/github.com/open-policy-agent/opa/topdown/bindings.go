// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"

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
	values *util.HashMap
}

func newBindings() *bindings {

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

	return &bindings{values}
}

func (u *bindings) Iter(iter func(*ast.Term, *ast.Term) error) error {

	var err error

	u.values.Iter(func(k, v util.T) bool {
		if err != nil {
			return true
		}
		term := k.(*ast.Term)
		err = iter(term, u.Plug(term))
		return false
	})

	return err
}

func (u *bindings) Plug(a *ast.Term) *ast.Term {
	switch v := a.Value.(type) {
	case ast.Var:
		b, next := u.apply(a)
		if a != b || u != next {
			return next.Plug(b)
		}
		return b
	case ast.Array:
		cpy := *a
		arr := make(ast.Array, len(v))
		for i := 0; i < len(arr); i++ {
			arr[i] = u.Plug(v[i])
		}
		cpy.Value = arr
		return &cpy
	case ast.Object:
		cpy := *a
		obj := make(ast.Object, len(v))
		for i := 0; i < len(obj); i++ {
			obj[i] = ast.Item(u.Plug(v[i][0]), u.Plug(v[i][1]))
		}
		cpy.Value = obj
		return &cpy
	case *ast.Set:
		cpy := *a
		cpy.Value, _ = v.Map(func(x *ast.Term) (*ast.Term, error) {
			return u.Plug(x), nil
		})
		return &cpy
	}
	return a
}

func (u *bindings) String() string {
	if u == nil {
		return "{}"
	}
	return u.values.String()
}

func (u *bindings) bind(a *ast.Term, b *ast.Term, other *bindings) *undo {
	// fmt.Println("bind:", a, b)
	u.values.Put(a, value{
		u: other,
		v: b,
	})
	return &undo{a, u, nil}
}

func (u *bindings) apply(a *ast.Term) (*ast.Term, *bindings) {
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

type value struct {
	u *bindings
	v *ast.Term
}

func (v value) String() string {
	return fmt.Sprintf("<%v, %p>", v.v, v.u)
}

func (v value) equal(other *value) bool {
	if v.u == other.u {
		return v.v.Equal(other.v)
	}
	return false
}
