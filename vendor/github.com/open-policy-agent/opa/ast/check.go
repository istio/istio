// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
)

// exprChecker defines the interface for executing type checking on a single
// expression. The exprChecker must update the provided TypeEnv with inferred
// types of vars.
type exprChecker func(*TypeEnv, *Expr) *Error

// typeChecker implements type checking on queries and rules. Errors are
// accumulated on the typeChecker so that a single run can report multiple
// issues.
type typeChecker struct {
	errs         Errors
	exprCheckers map[string]exprChecker
}

// newTypeChecker returns a new typeChecker object that has no errors.
func newTypeChecker() *typeChecker {
	tc := &typeChecker{}
	tc.exprCheckers = map[string]exprChecker{
		"eq": tc.checkExprEq,
	}
	return tc
}

// CheckBody runs type checking on the body and returns a TypeEnv if no errors
// are found. The resulting TypeEnv wraps the provided one. The resulting
// TypeEnv will be able to resolve types of vars contained in the body.
func (tc *typeChecker) CheckBody(env *TypeEnv, body Body) (*TypeEnv, Errors) {

	if env == nil {
		env = NewTypeEnv()
	} else {
		env = env.wrap()
	}

	WalkExprs(body, func(expr *Expr) bool {

		closureErrs := tc.checkClosures(env, expr)
		for _, err := range closureErrs {
			tc.err(err)
		}

		hasClosureErrors := len(closureErrs) > 0

		vis := newRefChecker(env)
		Walk(vis, expr)
		for _, err := range vis.errs {
			tc.err(err)
		}

		hasRefErrors := len(vis.errs) > 0

		if err := tc.checkExpr(env, expr); err != nil {
			// Suppress this error if a more actionable one has occurred. In
			// this case, if an error occurred in a ref or closure contained in
			// this expression, and the error is due to a nil type, then it's
			// likely to be the result of the more specific error.
			skip := (hasClosureErrors || hasRefErrors) && causedByNilType(err)
			if !skip {
				tc.err(err)
			}
		}

		return true
	})

	return env, tc.errs
}

// CheckTypes runs type checking on the rules returns a TypeEnv if no errors
// are found. The resulting TypeEnv wraps the provided one. The resulting
// TypeEnv will be able to resolve types of refs that refer to rules.
func (tc *typeChecker) CheckTypes(env *TypeEnv, sorted []util.T) (*TypeEnv, Errors) {
	if env == nil {
		env = NewTypeEnv()
	} else {
		env = env.wrap()
	}
	for _, s := range sorted {
		tc.checkRule(env, s.(*Rule))
	}
	return env, tc.errs
}

func (tc *typeChecker) checkClosures(env *TypeEnv, expr *Expr) Errors {
	var result Errors
	WalkClosures(expr, func(x interface{}) bool {
		switch x := x.(type) {
		case *ArrayComprehension:
			_, errs := newTypeChecker().CheckBody(env, x.Body)
			if len(errs) > 0 {
				result = errs
				return true
			}
		case *SetComprehension:
			_, errs := newTypeChecker().CheckBody(env, x.Body)
			if len(errs) > 0 {
				result = errs
				return true
			}
		case *ObjectComprehension:
			_, errs := newTypeChecker().CheckBody(env, x.Body)
			if len(errs) > 0 {
				result = errs
				return true
			}
		}
		return false
	})
	return result
}

func (tc *typeChecker) checkLanguageBuiltins() *TypeEnv {
	env := NewTypeEnv()
	for _, bi := range Builtins {
		env.tree.Put(bi.Ref(), bi.Decl)
	}
	return env
}

func (tc *typeChecker) checkRule(env *TypeEnv, rule *Rule) {

	cpy, err := tc.CheckBody(env, rule.Body)

	if len(err) == 0 {

		path := rule.Path()
		var tpe types.Type

		if len(rule.Head.Args) > 0 {

			// If args are not referred to in body, infer as any.
			WalkVars(rule.Head.Args, func(v Var) bool {
				if cpy.Get(v) == nil {
					cpy.tree.PutOne(v, types.A)
				}
				return false
			})

			// Construct function type.
			args := make([]types.Type, len(rule.Head.Args))
			for i := 0; i < len(rule.Head.Args); i++ {
				args[i] = cpy.Get(rule.Head.Args[i])
			}

			f := types.NewFunction(args, cpy.Get(rule.Head.Value))

			// Union with existing.
			exist := env.tree.Get(path)
			tpe = types.Or(exist, f)

		} else {
			switch rule.Head.DocKind() {
			case CompleteDoc:
				typeV := cpy.Get(rule.Head.Value)
				if typeV != nil {
					exist := env.tree.Get(path)
					tpe = types.Or(typeV, exist)
				}
			case PartialObjectDoc:
				typeK := cpy.Get(rule.Head.Key)
				typeV := cpy.Get(rule.Head.Value)
				if typeK != nil && typeV != nil {
					exist := env.tree.Get(path)
					typeV = types.Or(types.Values(exist), typeV)
					typeK = types.Or(types.Keys(exist), typeK)
					tpe = types.NewObject(nil, types.NewDynamicProperty(typeK, typeV))
				}
			case PartialSetDoc:
				typeK := cpy.Get(rule.Head.Key)
				if typeK != nil {
					exist := env.tree.Get(path)
					typeK = types.Or(types.Keys(exist), typeK)
					tpe = types.NewSet(typeK)
				}
			}
		}

		if tpe != nil {
			env.tree.Put(path, tpe)
		}
	}
}

func (tc *typeChecker) checkExpr(env *TypeEnv, expr *Expr) *Error {
	if !expr.IsCall() {
		return nil
	}

	checker := tc.exprCheckers[expr.Operator().String()]
	if checker != nil {
		return checker(env, expr)
	}

	return tc.checkExprBuiltin(env, expr)
}

func (tc *typeChecker) checkExprBuiltin(env *TypeEnv, expr *Expr) *Error {

	args := expr.Operands()
	pre := make([]types.Type, len(args))
	for i := range args {
		pre[i] = env.Get(args[i])
	}

	name := expr.Operator()
	tpe := env.Get(name)

	if tpe == nil {
		return NewError(TypeErr, expr.Location, "undefined function %v", name)
	}

	ftpe, ok := tpe.(*types.Function)
	if !ok {
		return NewError(TypeErr, expr.Location, "undefined function %v", name)
	}

	maxArgs := len(ftpe.Args())
	expArgs := ftpe.Args()

	if ftpe.Result() != nil {
		maxArgs++
		expArgs = append(expArgs, ftpe.Result())
	}

	if len(args) > maxArgs {
		return newArgError(expr.Location, name, "too many arguments", pre, expArgs)
	} else if len(args) < len(ftpe.Args()) {
		return newArgError(expr.Location, name, "too few arguments", pre, expArgs)
	}

	for i := range args {
		if !unify1(env, args[i], expArgs[i], false) {
			post := make([]types.Type, len(args))
			for i := range args {
				post[i] = env.Get(args[i])
			}
			return newArgError(expr.Location, name, "invalid argument(s)", post, expArgs)
		}
	}

	return nil
}

func (tc *typeChecker) checkExprEq(env *TypeEnv, expr *Expr) *Error {

	a, b := expr.Operand(0), expr.Operand(1)
	typeA, typeB := env.Get(a), env.Get(b)

	if !unify2(env, a, typeA, b, typeB) {
		err := NewError(TypeErr, expr.Location, "match error")
		err.Details = &UnificationErrDetail{
			Left:  typeA,
			Right: typeB,
		}
		return err
	}

	return nil
}

func unify2(env *TypeEnv, a *Term, typeA types.Type, b *Term, typeB types.Type) bool {

	nilA := types.Nil(typeA)
	nilB := types.Nil(typeB)

	if nilA && !nilB {
		return unify1(env, a, typeB, false)
	} else if nilB && !nilA {
		return unify1(env, b, typeA, false)
	} else if !nilA && !nilB {
		return unifies(typeA, typeB)
	}

	switch a.Value.(type) {
	case Array:
		return unify2Array(env, a, typeA, b, typeB)
	case Object:
		return unify2Object(env, a, typeA, b, typeB)
	case Var:
		switch b.Value.(type) {
		case Var:
			return unify1(env, a, types.A, false) && unify1(env, b, env.Get(a), false)
		case Array:
			return unify2Array(env, b, typeB, a, typeA)
		case Object:
			return unify2Object(env, b, typeB, a, typeA)
		}
	}

	return false
}

func unify2Array(env *TypeEnv, a *Term, typeA types.Type, b *Term, typeB types.Type) bool {
	arr := a.Value.(Array)
	switch bv := b.Value.(type) {
	case Array:
		if len(arr) == len(bv) {
			for i := range arr {
				if !unify2(env, arr[i], env.Get(arr[i]), bv[i], env.Get(bv[i])) {
					return false
				}
			}
			return true
		}
	case Var:
		return unify1(env, a, types.A, false) && unify1(env, b, env.Get(a), false)
	}
	return false
}

func unify2Object(env *TypeEnv, a *Term, typeA types.Type, b *Term, typeB types.Type) bool {
	obj := a.Value.(Object)
	switch bv := b.Value.(type) {
	case Object:
		cv := obj.Intersect(bv)
		if obj.Len() == bv.Len() && bv.Len() == len(cv) {
			for i := range cv {
				if !unify2(env, cv[i][1], env.Get(cv[i][1]), cv[i][2], env.Get(cv[i][2])) {
					return false
				}
			}
			return true
		}
	case Var:
		return unify1(env, a, types.A, false) && unify1(env, b, env.Get(a), false)
	}
	return false
}

func unify1(env *TypeEnv, term *Term, tpe types.Type, union bool) bool {
	switch v := term.Value.(type) {
	case Array:
		switch tpe := tpe.(type) {
		case *types.Array:
			return unify1Array(env, v, tpe, union)
		case types.Any:
			if types.Compare(tpe, types.A) == 0 {
				for i := range v {
					unify1(env, v[i], types.A, true)
				}
				return true
			}
			unifies := false
			for i := range tpe {
				unifies = unify1(env, term, tpe[i], true) || unifies
			}
			return unifies
		}
		return false
	case Object:
		switch tpe := tpe.(type) {
		case *types.Object:
			return unify1Object(env, v, tpe, union)
		case types.Any:
			if types.Compare(tpe, types.A) == 0 {
				v.Foreach(func(_, value *Term) {
					unify1(env, value, types.A, true)
				})
				return true
			}
			unifies := false
			for i := range tpe {
				unifies = unify1(env, term, tpe[i], true) || unifies
			}
			return unifies
		}
		return false
	case Set:
		switch tpe := tpe.(type) {
		case *types.Set:
			return unify1Set(env, v, tpe, union)
		case types.Any:
			if types.Compare(tpe, types.A) == 0 {
				v.Foreach(func(elem *Term) {
					unify1(env, elem, types.A, true)
				})
				return true
			}
			unifies := false
			for i := range tpe {
				unifies = unify1(env, term, tpe[i], true) || unifies
			}
			return unifies
		}
		return false
	case Ref, *ArrayComprehension, *ObjectComprehension, *SetComprehension:
		return unifies(env.Get(v), tpe)
	case Var:
		if !union {
			if exist := env.Get(v); exist != nil {
				return unifies(exist, tpe)
			}
			env.tree.PutOne(term.Value, tpe)
		} else {
			env.tree.PutOne(term.Value, types.Or(env.Get(v), tpe))
		}
		return true
	default:
		if !IsConstant(v) {
			panic("unreachable")
		}
		return unifies(env.Get(term), tpe)
	}
}

func unify1Array(env *TypeEnv, val Array, tpe *types.Array, union bool) bool {
	if len(val) != tpe.Len() && tpe.Dynamic() == nil {
		return false
	}
	for i := range val {
		if !unify1(env, val[i], tpe.Select(i), union) {
			return false
		}
	}
	return true
}

func unify1Object(env *TypeEnv, val Object, tpe *types.Object, union bool) bool {
	if val.Len() != len(tpe.Keys()) && tpe.DynamicValue() == nil {
		return false
	}
	stop := val.Until(func(k, v *Term) bool {
		if IsConstant(k.Value) {
			if child := selectConstant(tpe, k); child != nil {
				if !unify1(env, v, child, union) {
					return true
				}
			} else {
				return true
			}
		} else {
			// Inferring type of value under dynamic key would involve unioning
			// with all property values of tpe whose keys unify. For now, type
			// these values as Any. We can investigate stricter inference in
			// the future.
			unify1(env, v, types.A, union)
		}
		return false
	})
	return !stop
}

func unify1Set(env *TypeEnv, val Set, tpe *types.Set, union bool) bool {
	of := types.Values(tpe)
	return !val.Until(func(elem *Term) bool {
		return !unify1(env, elem, of, union)
	})
}

func (tc *typeChecker) err(err *Error) {
	tc.errs = append(tc.errs, err)
}

type refChecker struct {
	env       *TypeEnv
	errs      Errors
	checkTerm bool
}

func newRefChecker(env *TypeEnv) *refChecker {
	return &refChecker{
		env:  env,
		errs: nil,
	}
}

func (rc *refChecker) Visit(x interface{}) Visitor {
	switch x := x.(type) {
	case *ArrayComprehension, *ObjectComprehension, *SetComprehension:
		return nil
	case *Expr:
		switch terms := x.Terms.(type) {
		case []*Term:
			for i := 1; i < len(terms); i++ {
				Walk(rc, terms[i])
			}
			return nil
		case *Term:
			rc.checkTerm = true
			Walk(rc, terms)
			rc.checkTerm = false
			return nil
		}
	case Ref:
		if rc.checkTerm {
			if err := rc.checkApply(rc.env, x); err != nil {
				rc.errs = append(rc.errs, err)
				return nil
			}
		}
		if err := rc.checkRef(rc.env, rc.env.tree, x, 0); err != nil {
			rc.errs = append(rc.errs, err)
		}
	}
	return rc
}

func (rc *refChecker) checkApply(curr *TypeEnv, ref Ref) *Error {
	if tpe := curr.Get(ref); tpe != nil {
		if _, ok := tpe.(*types.Function); ok {
			return newRefErrUnsupported(ref[0].Location, ref, len(ref)-1, tpe)
		}
	}
	return nil
}

func (rc *refChecker) checkRef(curr *TypeEnv, node *typeTreeNode, ref Ref, idx int) *Error {

	if idx == len(ref) {
		return nil
	}

	head := ref[idx]

	// Handle constant ref operands, i.e., strings or the ref head.
	if _, ok := head.Value.(String); ok || idx == 0 {

		child := node.Child(head.Value)
		if child == nil {

			if curr.next != nil {
				next := curr.next
				return rc.checkRef(next, next.tree, ref, 0)
			}

			if RootDocumentNames.Contains(ref[0]) {
				return rc.checkRefLeaf(types.A, ref, 1)
			}

			return rc.checkRefLeaf(types.A, ref, 0)
		}

		if child.Leaf() {
			return rc.checkRefLeaf(child.Value(), ref, idx+1)
		}

		return rc.checkRef(curr, child, ref, idx+1)
	}

	// Handle dynamic ref operands.
	switch value := head.Value.(type) {

	case Var:

		if exist := rc.env.Get(value); exist != nil {
			if !unifies(types.S, exist) {
				return newRefErrInvalid(ref[0].Location, ref, idx, exist, types.S, getOneOfForNode(node))
			}
		} else {
			rc.env.tree.PutOne(value, types.S)
		}

	case Ref:

		exist := rc.env.Get(value)
		if exist == nil {
			// If ref type is unknown, an error will already be reported so
			// stop here.
			return nil
		}

		if !unifies(types.S, exist) {
			return newRefErrInvalid(ref[0].Location, ref, idx, exist, types.S, getOneOfForNode(node))
		}

	// Catch other ref operand types here. Non-leaf nodes must be referred to
	// with string values.
	default:
		return newRefErrInvalid(ref[0].Location, ref, idx, nil, types.S, getOneOfForNode(node))
	}

	// Run checking on remaining portion of the ref. Note, since the ref
	// potentially refers to data for which no type information exists,
	// checking should never fail.
	node.Children().Iter(func(_, child util.T) bool {
		rc.checkRef(curr, child.(*typeTreeNode), ref, idx+1)
		return false
	})

	return nil
}

func (rc *refChecker) checkRefLeaf(tpe types.Type, ref Ref, idx int) *Error {

	if idx == len(ref) {
		return nil
	}

	head := ref[idx]

	keys := types.Keys(tpe)
	if keys == nil {
		return newRefErrUnsupported(ref[0].Location, ref, idx-1, tpe)
	}

	switch value := head.Value.(type) {

	case Var:
		if exist := rc.env.Get(value); exist != nil {
			if !unifies(exist, keys) {
				return newRefErrInvalid(ref[0].Location, ref, idx, exist, keys, getOneOfForType(tpe))
			}
		} else {
			rc.env.tree.PutOne(value, types.Keys(tpe))
		}

	case Ref:
		if exist := rc.env.Get(value); exist != nil {
			if !unifies(exist, keys) {
				return newRefErrInvalid(ref[0].Location, ref, idx, exist, keys, getOneOfForType(tpe))
			}
		}

	case Array, Object, Set:
		// Composite references operands may only be used with a set.
		if !unifies(tpe, types.NewSet(types.A)) {
			return newRefErrInvalid(ref[0].Location, ref, idx, tpe, types.NewSet(types.A), nil)
		}
		if !unify1(rc.env, head, keys, false) {
			return newRefErrInvalid(ref[0].Location, ref, idx, rc.env.Get(head), keys, nil)
		}

	default:
		child := selectConstant(tpe, head)
		if child == nil {
			return newRefErrInvalid(ref[0].Location, ref, idx, nil, types.Keys(tpe), getOneOfForType(tpe))
		}
		return rc.checkRefLeaf(child, ref, idx+1)
	}

	return rc.checkRefLeaf(types.Values(tpe), ref, idx+1)
}

func unifies(a, b types.Type) bool {

	if a == nil || b == nil {
		return false
	}

	anyA, ok1 := a.(types.Any)
	if ok1 {
		if unifiesAny(anyA, b) {
			return true
		}
	}

	anyB, ok2 := b.(types.Any)
	if ok2 {
		if unifiesAny(anyB, a) {
			return true
		}
	}

	if ok1 || ok2 {
		return false
	}

	switch a := a.(type) {
	case types.Null:
		_, ok := b.(types.Null)
		return ok
	case types.Boolean:
		_, ok := b.(types.Boolean)
		return ok
	case types.Number:
		_, ok := b.(types.Number)
		return ok
	case types.String:
		_, ok := b.(types.String)
		return ok
	case *types.Array:
		b, ok := b.(*types.Array)
		if !ok {
			return false
		}
		return unifiesArrays(a, b)
	case *types.Object:
		b, ok := b.(*types.Object)
		if !ok {
			return false
		}
		return unifiesObjects(a, b)
	case *types.Set:
		b, ok := b.(*types.Set)
		if !ok {
			return false
		}
		return unifies(types.Values(a), types.Values(b))
	case *types.Function:
		// TODO(tsandall): revisit once functions become first-class values.
		return false
	default:
		panic("unreachable")
	}
}

func unifiesAny(a types.Any, b types.Type) bool {
	if _, ok := b.(*types.Function); ok {
		return false
	}
	for i := range a {
		if unifies(a[i], b) {
			return true
		}
	}
	return len(a) == 0
}

func unifiesArrays(a, b *types.Array) bool {

	if !unifiesArraysStatic(a, b) {
		return false
	}

	if !unifiesArraysStatic(b, a) {
		return false
	}

	return a.Dynamic() == nil || b.Dynamic() == nil || unifies(a.Dynamic(), b.Dynamic())
}

func unifiesArraysStatic(a, b *types.Array) bool {
	if a.Len() != 0 {
		for i := 0; i < a.Len(); i++ {
			if !unifies(a.Select(i), b.Select(i)) {
				return false
			}
		}
	}
	return true
}

func unifiesObjects(a, b *types.Object) bool {
	if !unifiesObjectsStatic(a, b) {
		return false
	}

	if !unifiesObjectsStatic(b, a) {
		return false
	}

	return a.DynamicValue() == nil || b.DynamicValue() == nil || unifies(a.DynamicValue(), b.DynamicValue())
}

func unifiesObjectsStatic(a, b *types.Object) bool {
	for _, k := range a.Keys() {
		if !unifies(a.Select(k), b.Select(k)) {
			return false
		}
	}
	return true
}

// typeErrorCause defines an interface to determine the reason for a type
// error. The type error details implement this interface so that type checking
// can report more actionable errors.
type typeErrorCause interface {
	nilType() bool
}

func causedByNilType(err *Error) bool {
	cause, ok := err.Details.(typeErrorCause)
	if !ok {
		return false
	}
	return cause.nilType()
}

// ArgErrDetail represents a generic argument error.
type ArgErrDetail struct {
	Have []types.Type `json:"have"`
	Want []types.Type `json:"want"`
}

// Lines returns the string representation of the detail.
func (d *ArgErrDetail) Lines() []string {
	lines := make([]string, 2)
	lines[0] = fmt.Sprint("have: ", formatArgs(d.Have))
	lines[1] = fmt.Sprint("want: ", formatArgs(d.Want))
	return lines
}

func (d *ArgErrDetail) nilType() bool {
	for i := range d.Have {
		if types.Nil(d.Have[i]) {
			return true
		}
	}
	return false
}

// UnificationErrDetail describes a type mismatch error when two values are
// unified (e.g., x = [1,2,y]).
type UnificationErrDetail struct {
	Left  types.Type `json:"a"`
	Right types.Type `json:"b"`
}

func (a *UnificationErrDetail) nilType() bool {
	return types.Nil(a.Left) || types.Nil(a.Right)
}

// Lines returns the string representation of the detail.
func (a *UnificationErrDetail) Lines() []string {
	lines := make([]string, 2)
	lines[0] = fmt.Sprint("left  : ", types.Sprint(a.Left))
	lines[1] = fmt.Sprint("right : ", types.Sprint(a.Right))
	return lines
}

// RefErrUnsupportedDetail describes an undefined reference error where the
// referenced value does not support dereferencing (e.g., scalars).
type RefErrUnsupportedDetail struct {
	Ref  Ref        `json:"ref"`  // invalid ref
	Pos  int        `json:"pos"`  // invalid element
	Have types.Type `json:"have"` // referenced type
}

// Lines returns the string representation of the detail.
func (r *RefErrUnsupportedDetail) Lines() []string {
	lines := []string{
		r.Ref.String(),
		strings.Repeat("^", len(r.Ref[:r.Pos+1].String())),
		fmt.Sprintf("have: %v", r.Have),
	}
	return lines
}

// RefErrInvalidDetail describes an undefined reference error where the referenced
// value does not support the reference operand (e.g., missing object key,
// invalid key type, etc.)
type RefErrInvalidDetail struct {
	Ref   Ref        `json:"ref"`            // invalid ref
	Pos   int        `json:"pos"`            // invalid element
	Have  types.Type `json:"have,omitempty"` // type of invalid element (for var/ref elements)
	Want  types.Type `json:"want"`           // allowed type (for non-object values)
	OneOf []Value    `json:"oneOf"`          // allowed values (e.g., for object keys)
}

// Lines returns the string representation of the detail.
func (r *RefErrInvalidDetail) Lines() []string {
	lines := []string{r.Ref.String()}
	offset := len(r.Ref[:r.Pos].String()) + 1
	pad := strings.Repeat(" ", offset)
	lines = append(lines, fmt.Sprintf("%s^", pad))
	if r.Have != nil {
		lines = append(lines, fmt.Sprintf("%shave (type): %v", pad, r.Have))
	} else {
		lines = append(lines, fmt.Sprintf("%shave: %v", pad, r.Ref[r.Pos]))
	}
	if len(r.OneOf) > 0 {
		lines = append(lines, fmt.Sprintf("%swant (one of): %v", pad, r.OneOf))
	} else {
		lines = append(lines, fmt.Sprintf("%swant (type): %v", pad, r.Want))
	}
	return lines
}

func formatArgs(args []types.Type) string {
	buf := make([]string, len(args))
	for i := range args {
		buf[i] = types.Sprint(args[i])
	}
	return "(" + strings.Join(buf, ", ") + ")"
}

func newRefErrInvalid(loc *Location, ref Ref, idx int, have, want types.Type, oneOf []Value) *Error {
	err := newRefError(loc, ref)
	err.Details = &RefErrInvalidDetail{
		Ref:   ref,
		Pos:   idx,
		Have:  have,
		Want:  want,
		OneOf: oneOf,
	}
	return err
}

func newRefErrUnsupported(loc *Location, ref Ref, idx int, have types.Type) *Error {
	err := newRefError(loc, ref)
	err.Details = &RefErrUnsupportedDetail{
		Ref:  ref,
		Pos:  idx,
		Have: have,
	}
	return err
}

func newRefError(loc *Location, ref Ref) *Error {
	return NewError(TypeErr, loc, "undefined ref: %v", ref)
}

func newArgError(loc *Location, builtinName Ref, msg string, have []types.Type, want []types.Type) *Error {
	err := NewError(TypeErr, loc, "%v: %v", builtinName, msg)
	err.Details = &ArgErrDetail{
		Have: have,
		Want: want,
	}
	return err
}

func getOneOfForNode(node *typeTreeNode) (result []Value) {
	node.Children().Iter(func(k, _ util.T) bool {
		result = append(result, k.(Value))
		return false
	})

	sortValueSlice(result)
	return result
}

func getOneOfForType(tpe types.Type) (result []Value) {
	switch tpe := tpe.(type) {
	case *types.Object:
		for _, k := range tpe.Keys() {
			v, err := InterfaceToValue(k)
			if err != nil {
				panic(err)
			}
			result = append(result, v)
		}
	}
	sortValueSlice(result)
	return result
}

func sortValueSlice(sl []Value) {
	sort.Slice(sl, func(i, j int) bool {
		return sl[i].Compare(sl[j]) < 0
	})
}
