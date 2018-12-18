// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package checker defines functions to type-checked a parsed expression
// against a set of identifier and function declarations.
package checker

import (
	"fmt"
	"reflect"

	"github.com/golang/protobuf/proto"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/types/ref"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type checker struct {
	env                *Env
	errors             *typeErrors
	mappings           *mapping
	freeTypeVarCounter int
	sourceInfo         *exprpb.SourceInfo

	types      map[int64]*exprpb.Type
	references map[int64]*exprpb.Reference
}

// Check performs type checking, giving a typed AST.
// The input is a ParsedExpr proto and an env which encapsulates
// type binding of variables, declarations of built-in functions,
// descriptions of protocol buffers, and a registry for errors.
// Returns a CheckedExpr proto, which might not be usable if
// there are errors in the error registry.
func Check(parsedExpr *exprpb.ParsedExpr,
	source common.Source,
	env *Env) (*exprpb.CheckedExpr, *common.Errors) {
	c := checker{
		env:                env,
		errors:             &typeErrors{common.NewErrors(source)},
		mappings:           newMapping(),
		freeTypeVarCounter: 0,
		sourceInfo:         parsedExpr.GetSourceInfo(),

		types:      make(map[int64]*exprpb.Type),
		references: make(map[int64]*exprpb.Reference),
	}
	c.check(parsedExpr.GetExpr())

	// Walk over the final type map substituting any type parameters either by their bound value or
	// by DYN.
	m := make(map[int64]*exprpb.Type)
	for k, v := range c.types {
		m[k] = substitute(c.mappings, v, true)
	}

	return &exprpb.CheckedExpr{
		Expr:         parsedExpr.GetExpr(),
		SourceInfo:   parsedExpr.GetSourceInfo(),
		TypeMap:      m,
		ReferenceMap: c.references,
	}, c.errors.Errors
}

func (c *checker) check(e *exprpb.Expr) {
	if e == nil {
		return
	}

	switch e.ExprKind.(type) {
	case *exprpb.Expr_ConstExpr:
		literal := e.GetConstExpr()
		switch literal.ConstantKind.(type) {
		case *exprpb.Constant_BoolValue:
			c.checkBoolLiteral(e)
		case *exprpb.Constant_BytesValue:
			c.checkBytesLiteral(e)
		case *exprpb.Constant_DoubleValue:
			c.checkDoubleLiteral(e)
		case *exprpb.Constant_Int64Value:
			c.checkInt64Literal(e)
		case *exprpb.Constant_NullValue:
			c.checkNullLiteral(e)
		case *exprpb.Constant_StringValue:
			c.checkStringLiteral(e)
		case *exprpb.Constant_Uint64Value:
			c.checkUint64Literal(e)
		}
	case *exprpb.Expr_IdentExpr:
		c.checkIdent(e)
	case *exprpb.Expr_SelectExpr:
		c.checkSelect(e)
	case *exprpb.Expr_CallExpr:
		c.checkCall(e)
	case *exprpb.Expr_ListExpr:
		c.checkCreateList(e)
	case *exprpb.Expr_StructExpr:
		c.checkCreateStruct(e)
	case *exprpb.Expr_ComprehensionExpr:
		c.checkComprehension(e)
	default:
		panic(fmt.Sprintf("Unrecognized ast type: %v", reflect.TypeOf(e)))
	}
}

func (c *checker) checkInt64Literal(e *exprpb.Expr) {
	c.setType(e, decls.Int)
}

func (c *checker) checkUint64Literal(e *exprpb.Expr) {
	c.setType(e, decls.Uint)
}

func (c *checker) checkStringLiteral(e *exprpb.Expr) {
	c.setType(e, decls.String)
}

func (c *checker) checkBytesLiteral(e *exprpb.Expr) {
	c.setType(e, decls.Bytes)
}

func (c *checker) checkDoubleLiteral(e *exprpb.Expr) {
	c.setType(e, decls.Double)
}

func (c *checker) checkBoolLiteral(e *exprpb.Expr) {
	c.setType(e, decls.Bool)
}

func (c *checker) checkNullLiteral(e *exprpb.Expr) {
	c.setType(e, decls.Null)
}

func (c *checker) checkIdent(e *exprpb.Expr) {
	identExpr := e.GetIdentExpr()
	if ident := c.env.LookupIdent(identExpr.Name); ident != nil {
		c.setType(e, ident.GetIdent().Type)
		c.setReference(e, newIdentReference(ident.Name, ident.GetIdent().Value))
		return
	}

	c.setType(e, decls.Error)
	c.errors.undeclaredReference(
		c.location(e), c.env.packager.Package(), identExpr.Name)
}

func (c *checker) checkSelect(e *exprpb.Expr) {
	sel := e.GetSelectExpr()
	// Before traversing down the tree, try to interpret as qualified name.
	qname, found := toQualifiedName(e)
	if found {
		ident := c.env.LookupIdent(qname)
		if ident != nil {
			if sel.TestOnly {
				c.errors.expressionDoesNotSelectField(c.location(e))
				c.setType(e, decls.Bool)
			} else {
				c.setType(e, ident.GetIdent().Type)
				c.setReference(e,
					newIdentReference(ident.Name, ident.GetIdent().Value))
			}
			return
		}
	}

	// Interpret as field selection, first traversing down the operand.
	c.check(sel.Operand)
	targetType := c.getType(sel.Operand)
	// Assume error type by default as most types do not support field selection.
	resultType := decls.Error
	switch kindOf(targetType) {
	case kindMap:
		// Maps yeild their value type as the selection result type.
		mapType := targetType.GetMapType()
		resultType = mapType.ValueType
	case kindObject:
		// Objects yield their field type declaration as the selection result type, but only if
		// the field is defined.
		messageType := targetType
		if fieldType, found := c.lookupFieldType(c.location(e), messageType, sel.Field); found {
			resultType = fieldType.Type
			// In proto3, primitive field types can't support presence testing, so the has()
			// style operation would be invalid in this instance.
			if sel.TestOnly && !fieldType.SupportsPresence {
				c.errors.fieldDoesNotSupportPresenceCheck(c.location(e), sel.Field)
			}
		}
	case kindTypeParam:
		// Type params are expected to be the same type as the target.
		resultType = targetType
	default:
		// Dynamic / error values are treated as DYN type. Errors are handled this way as well
		// in order to allow forward progress on the check.
		if isDynOrError(targetType) {
			resultType = decls.Dyn
		} else {
			c.errors.typeDoesNotSupportFieldSelection(c.location(e), targetType)
		}
	}
	if sel.TestOnly {
		resultType = decls.Bool
	}
	c.setType(e, resultType)
}

func (c *checker) checkCall(e *exprpb.Expr) {
	call := e.GetCallExpr()
	// Traverse arguments.
	for _, arg := range call.Args {
		c.check(arg)
	}

	var resolution *overloadResolution

	if call.Target == nil {
		// Regular static call with simple name.
		if fn := c.env.LookupFunction(call.Function); fn != nil {
			resolution = c.resolveOverload(c.location(e), fn, nil, call.Args)
		} else {
			c.errors.undeclaredReference(
				c.location(e), c.env.packager.Package(), call.Function)
		}
	} else {
		// Check whether the target is actually a qualified name for a static function.
		if qname, found := toQualifiedName(call.Target); found {
			fn := c.env.LookupFunction(qname + "." + call.Function)
			if fn != nil {
				resolution = c.resolveOverload(c.location(e), fn, nil, call.Args)
			}
		}

		if resolution == nil {
			// Regular instance call.
			c.check(call.Target)

			if fn := c.env.LookupFunction(call.Function); fn != nil {
				resolution = c.resolveOverload(c.location(e), fn, call.Target, call.Args)
			} else {
				c.errors.undeclaredReference(
					c.location(e), c.env.packager.Package(), call.Function)
			}
		}
	}

	if resolution != nil {
		c.setType(e, resolution.Type)
		c.setReference(e, resolution.Reference)
	} else {
		c.setType(e, decls.Error)
	}
}

func (c *checker) resolveOverload(
	loc common.Location,
	fn *exprpb.Decl, target *exprpb.Expr, args []*exprpb.Expr) *overloadResolution {

	var argTypes []*exprpb.Type
	if target != nil {
		argTypes = append(argTypes, c.getType(target))
	}
	for _, arg := range args {
		argTypes = append(argTypes, c.getType(arg))
	}

	var resultType *exprpb.Type
	var checkedRef *exprpb.Reference
	for _, overload := range fn.GetFunction().Overloads {
		if (target == nil && overload.IsInstanceFunction) ||
			(target != nil && !overload.IsInstanceFunction) {
			// not a compatible call style.
			continue
		}

		overloadType := decls.NewFunctionType(overload.ResultType, overload.Params...)
		if len(overload.TypeParams) > 0 {
			// Instantiate overload's type with fresh type variables.
			substitutions := newMapping()
			for _, typePar := range overload.TypeParams {
				substitutions.add(decls.NewTypeParamType(typePar), c.newTypeVar())
			}

			overloadType = substitute(substitutions, overloadType, false)
		}

		candidateArgTypes := overloadType.GetFunction().ArgTypes
		if c.isAssignableList(argTypes, candidateArgTypes) {
			if checkedRef == nil {
				checkedRef = newFunctionReference(overload.OverloadId)
			} else {
				checkedRef.OverloadId = append(checkedRef.OverloadId, overload.OverloadId)
			}

			if resultType == nil {
				// First matching overload, determines result type.
				resultType = substitute(c.mappings,
					overloadType.GetFunction().ResultType,
					false)
			} else {
				// More than one matching overload, narrow result type to DYN.
				resultType = decls.Dyn
			}

		}
	}

	if resultType == nil {
		c.errors.noMatchingOverload(loc, fn.Name, argTypes, target != nil)
		resultType = decls.Error
		return nil
	}

	return newResolution(checkedRef, resultType)
}

func (c *checker) checkCreateList(e *exprpb.Expr) {
	create := e.GetListExpr()
	var elemType *exprpb.Type
	for _, e := range create.Elements {
		c.check(e)
		elemType = c.joinTypes(c.location(e), elemType, c.getType(e))
	}
	if elemType == nil {
		// If the list is empty, assign free type var to elem type.
		elemType = c.newTypeVar()
	}
	c.setType(e, decls.NewListType(elemType))
}

func (c *checker) checkCreateStruct(e *exprpb.Expr) {
	str := e.GetStructExpr()
	if str.MessageName != "" {
		c.checkCreateMessage(e)
	} else {
		c.checkCreateMap(e)
	}
}

func (c *checker) checkCreateMap(e *exprpb.Expr) {
	mapVal := e.GetStructExpr()
	var keyType *exprpb.Type
	var valueType *exprpb.Type
	for _, ent := range mapVal.GetEntries() {
		key := ent.GetMapKey()
		c.check(key)
		keyType = c.joinTypes(c.location(key), keyType, c.getType(key))

		c.check(ent.Value)
		valueType = c.joinTypes(c.location(ent.Value), valueType, c.getType(ent.Value))
	}
	if keyType == nil {
		// If the map is empty, assign free type variables to typeKey and value type.
		keyType = c.newTypeVar()
		valueType = c.newTypeVar()
	}
	c.setType(e, decls.NewMapType(keyType, valueType))
}

func (c *checker) checkCreateMessage(e *exprpb.Expr) {
	msgVal := e.GetStructExpr()
	// Determine the type of the message.
	messageType := decls.Error
	decl := c.env.LookupIdent(msgVal.MessageName)
	if decl == nil {
		c.errors.undeclaredReference(
			c.location(e), c.env.packager.Package(), msgVal.MessageName)
		return
	}

	c.setReference(e, newIdentReference(decl.Name, nil))
	ident := decl.GetIdent()
	identKind := kindOf(ident.Type)
	if identKind != kindError {
		if identKind != kindType {
			c.errors.notAType(c.location(e), ident.Type)
		} else {
			messageType = ident.Type.GetType()
			if kindOf(messageType) != kindObject {
				c.errors.notAMessageType(c.location(e), messageType)
				messageType = decls.Error
			}
		}
	}
	c.setType(e, messageType)

	// Check the field initializers.
	for _, ent := range msgVal.GetEntries() {
		field := ent.GetFieldKey()
		value := ent.Value
		c.check(value)

		fieldType := decls.Error
		if t, found := c.lookupFieldType(c.locationByID(ent.Id), messageType, field); found {
			fieldType = t.Type
		}
		if !c.isAssignable(fieldType, c.getType(value)) {
			c.errors.fieldTypeMismatch(c.locationByID(ent.Id), field, fieldType, c.getType(value))
		}
	}
}

func (c *checker) checkComprehension(e *exprpb.Expr) {
	comp := e.GetComprehensionExpr()
	c.check(comp.IterRange)
	c.check(comp.AccuInit)
	accuType := c.getType(comp.AccuInit)
	rangeType := c.getType(comp.IterRange)
	var varType *exprpb.Type

	switch kindOf(rangeType) {
	case kindList:
		varType = rangeType.GetListType().ElemType
	case kindMap:
		// Ranges over the keys.
		varType = rangeType.GetMapType().KeyType
	case kindDyn, kindError:
		varType = decls.Dyn
	default:
		c.errors.notAComprehensionRange(c.location(comp.IterRange), rangeType)
	}

	c.env.enterScope()
	c.env.Add(decls.NewIdent(comp.AccuVar, accuType, nil))
	// Declare iteration variable on inner scope.
	c.env.enterScope()
	c.env.Add(decls.NewIdent(comp.IterVar, varType, nil))
	c.check(comp.LoopCondition)
	c.assertType(comp.LoopCondition, decls.Bool)
	c.check(comp.LoopStep)
	c.assertType(comp.LoopStep, accuType)
	// Forget iteration variable, as result expression must only depend on accu.
	c.env.exitScope()
	c.check(comp.Result)
	c.env.exitScope()
	c.setType(e, c.getType(comp.Result))
}

// Checks compatibility of joined types, and returns the most general common type.
func (c *checker) joinTypes(loc common.Location, previous *exprpb.Type, current *exprpb.Type) *exprpb.Type {
	if previous == nil {
		return current
	}
	if !c.isAssignable(previous, current) {
		return decls.Dyn
	}
	return mostGeneral(previous, current)
}

func (c *checker) newTypeVar() *exprpb.Type {
	id := c.freeTypeVarCounter
	c.freeTypeVarCounter++
	return decls.NewTypeParamType(fmt.Sprintf("_var%d", id))
}

func (c *checker) isAssignable(t1 *exprpb.Type, t2 *exprpb.Type) bool {
	subs := isAssignable(c.mappings, t1, t2)
	if subs != nil {
		c.mappings = subs
		return true
	}

	return false
}

func (c *checker) isAssignableList(l1 []*exprpb.Type, l2 []*exprpb.Type) bool {
	subs := isAssignableList(c.mappings, l1, l2)
	if subs != nil {
		c.mappings = subs
		return true
	}

	return false
}

func (c *checker) lookupFieldType(l common.Location, messageType *exprpb.Type, fieldName string) (*ref.FieldType, bool) {
	if _, found := c.env.typeProvider.FindType(messageType.GetMessageType()); !found {
		// This should not happen, anyway, report an error.
		c.errors.unexpectedFailedResolution(l, messageType.GetMessageType())
		return nil, false
	}

	if ft, found := c.env.typeProvider.FindFieldType(messageType, fieldName); found {
		return ft, found
	}

	c.errors.undefinedField(l, fieldName)
	return nil, false
}

func (c *checker) setType(e *exprpb.Expr, t *exprpb.Type) {
	if old, found := c.types[e.Id]; found && !proto.Equal(old, t) {
		panic(fmt.Sprintf("(Incompatible) Type already exists for expression: %v(%d) old:%v, new:%v", e, e.Id, old, t))
	}
	c.types[e.Id] = t
}

func (c *checker) getType(e *exprpb.Expr) *exprpb.Type {
	return c.types[e.Id]
}

func (c *checker) setReference(e *exprpb.Expr, r *exprpb.Reference) {
	if old, found := c.references[e.Id]; found && !proto.Equal(old, r) {
		panic(fmt.Sprintf("Reference already exists for expression: %v(%d) old:%v, new:%v", e, e.Id, old, r))
	}
	c.references[e.Id] = r
}

func (c *checker) assertType(e *exprpb.Expr, t *exprpb.Type) {
	if !c.isAssignable(t, c.getType(e)) {
		c.errors.typeMismatch(c.location(e), t, c.getType(e))
	}
}

type overloadResolution struct {
	Reference *exprpb.Reference
	Type      *exprpb.Type
}

func newResolution(checkedRef *exprpb.Reference, t *exprpb.Type) *overloadResolution {
	return &overloadResolution{
		Reference: checkedRef,
		Type:      t,
	}
}

func (c *checker) location(e *exprpb.Expr) common.Location {
	return c.locationByID(e.Id)
}

func (c *checker) locationByID(id int64) common.Location {
	positions := c.sourceInfo.GetPositions()
	var line = 1
	var col = 0
	if offset, found := positions[id]; found {
		col = int(offset)
		for _, lineOffset := range c.sourceInfo.LineOffsets {
			if lineOffset < offset {
				line++
				col = int(offset - lineOffset)
			} else {
				break
			}
		}
		return common.NewLocation(line, col)
	}
	return common.NoLocation
}

func newIdentReference(name string, value *exprpb.Constant) *exprpb.Reference {
	return &exprpb.Reference{Name: name, Value: value}
}

func newFunctionReference(overloads ...string) *exprpb.Reference {
	return &exprpb.Reference{OverloadId: overloads}
}

// Attempt to interpret an expression as a qualified name. This traverses select and getIdent
// expression and returns the name they constitute, or null if the expression cannot be
// interpreted like this.
func toQualifiedName(e *exprpb.Expr) (string, bool) {
	switch e.ExprKind.(type) {
	case *exprpb.Expr_IdentExpr:
		i := e.GetIdentExpr()
		return i.Name, true
	case *exprpb.Expr_SelectExpr:
		s := e.GetSelectExpr()
		if qname, found := toQualifiedName(s.Operand); found {
			return qname + "." + s.Field, true
		}
	}
	return "", false
}
