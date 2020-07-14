// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cel

import (
	"errors"
	"reflect"
	"strings"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
	"github.com/google/cel-go/interpreter"
	"github.com/google/cel-go/parser"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"istio.io/api/policy/v1beta1"
	"istio.io/pkg/attribute"
)

// Attribute provider resolves typing information by modeling attributes
// as fields in nested proto messages with presence.
//
// For example, an integer attribute "a.b.c" is modeled as the following proto3 definition:
//
// message A {
//   message B {
//     google.protobuf.Int64Value c = 1;
//   }
//   B b = 1;
// }
//
// with root context containing a variable "a" of type "A".
//
// Synthetic message type names are dot-prepended attribute names, e.g. ".a.b.c".
type attributeProvider struct {
	// fallback proto-based type provider
	protos ref.TypeProvider

	root *node

	typeMap map[string]*node
}

// node corresponds to the message types holding other message types or scalars.
// leaf nodes have field types, inner nodes have children.
type node struct {
	typeName string

	children map[string]*node

	typ       *exprpb.Type
	valueType v1beta1.ValueType
}

func (n *node) HasTrait(trait int) bool {
	// only support getter trait
	return trait == traits.IndexerType || trait == traits.FieldTesterType
}
func (n *node) TypeName() string {
	return n.typeName
}

func (ap *attributeProvider) newNode(typeName string) *node {
	out := &node{typeName: typeName}
	ap.typeMap[typeName] = out
	return out
}

func (ap *attributeProvider) insert(n *node, words []string, valueType v1beta1.ValueType) {
	if len(words) == 0 {
		n.valueType = valueType
		n.typ = convertType(valueType)
		return
	}

	if n.children == nil {
		n.children = make(map[string]*node)
	}

	child, ok := n.children[words[0]]
	if !ok {
		child = ap.newNode(n.typeName + "." + words[0])
		n.children[words[0]] = child
	}

	ap.insert(child, words[1:], valueType)
}

func newAttributeProvider(attributes map[string]*v1beta1.AttributeManifest_AttributeInfo) *attributeProvider {
	out := &attributeProvider{
		protos:  types.NewRegistry(),
		typeMap: make(map[string]*node),
	}
	out.root = out.newNode("")
	for name, info := range attributes {
		out.insert(out.root, strings.Split(name, "."), info.ValueType)
	}
	return out
}

func (ap *attributeProvider) newEnvironment() *celgo.Env {
	var declarations []*exprpb.Decl

	// populate with root-level identifiers
	for name, node := range ap.root.children {
		if node.typ != nil {
			declarations = append(declarations, decls.NewIdent(name, node.typ, nil))
		} else {
			declarations = append(declarations, decls.NewIdent(name, decls.NewObjectType(node.typeName), nil))
		}
	}

	// populate with standard functions
	// error is never expected here
	env, _ := celgo.NewEnv(
		celgo.CustomTypeProvider(ap),
		celgo.Declarations(declarations...),
		celgo.Declarations(standardFunctions...),
		ext.Strings(),
		macros)

	return env
}

func (ap *attributeProvider) newActivation(bag attribute.Bag) interpreter.Activation {
	return attributeActivation{provider: ap, bag: bag}
}

func (ap *attributeProvider) EnumValue(enumName string) ref.Val {
	return ap.protos.EnumValue(enumName)
}
func (ap *attributeProvider) FindIdent(identName string) (ref.Val, bool) {
	return ap.protos.FindIdent(identName)
}
func (ap *attributeProvider) FindType(typeName string) (*exprpb.Type, bool) {
	if _, ok := ap.typeMap[typeName]; ok {
		return decls.NewObjectType(typeName), true
	}
	return ap.protos.FindType(typeName)
}
func (ap *attributeProvider) FindFieldType(messageName, fieldName string) (*ref.FieldType, bool) {
	node, ok := ap.typeMap[messageName]
	if !ok {
		return ap.protos.FindFieldType(messageName, fieldName)
	}

	child, ok := node.children[fieldName]
	if !ok {
		return nil, false
	}

	typ := child.typ
	if typ == nil {
		typ = decls.NewObjectType(child.typeName)
	}

	return &ref.FieldType{
			Type:             typ,
			SupportsPresence: true},
		true
}
func (ap *attributeProvider) NewValue(typeName string, fields map[string]ref.Val) ref.Val {
	return ap.protos.NewValue(typeName, fields)
}

// Attribute activation binds attribute values to the expression nodes
type attributeActivation struct {
	provider *attributeProvider
	bag      attribute.Bag
}

type value struct {
	node *node
	bag  attribute.Bag
}

func (v value) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	return nil, errors.New("cannot convert attribute message to native types")
}
func (v value) ConvertToType(typeValue ref.Type) ref.Val {
	return types.NewErr("cannot convert attribute message to CEL types")
}
func (v value) Equal(other ref.Val) ref.Val {
	return types.NewErr("attribute message does not support equality")
}
func (v value) Type() ref.Type {
	return v.node
}
func (v value) Value() interface{} {
	return v
}
func resolve(n *node, bag attribute.Bag) ref.Val {
	if n.typ == nil {
		return value{node: n, bag: bag}
	}
	value, found := bag.Get(n.typeName[1:])
	if found {
		return convertValue(n.valueType, value)
	}
	return defaultValue(n.valueType)
}

func (v value) Get(index ref.Val) ref.Val {
	if index.Type() != types.StringType {
		return types.NewErr("select not implemented")
	}

	field := index.Value().(string)
	child, ok := v.node.children[field]
	if !ok {
		return types.NewErr("cannot evaluate select of %q from %s", field, v.node.typeName)
	}

	return resolve(child, v.bag)
}

func (v value) IsSet(index ref.Val) ref.Val {
	if index.Type() != types.StringType {
		return types.NewErr("select tester not implemented")
	}
	field := index.Value().(string)
	child, ok := v.node.children[field]
	if !ok {
		return types.NewErr("cannot evaluate select of %q from %s", field, v.node.typeName)
	}

	if child.typ != nil {
		_, found := v.bag.Get(child.typeName[1:])
		return types.Bool(found)
	}

	// assume all intermediate nodes are set
	return types.True
}

func (a attributeActivation) ResolveName(name string) (interface{}, bool) {
	if node, ok := a.provider.root.children[name]; ok {
		return resolve(node, a.bag), true
	}
	return nil, false
}

func (a attributeActivation) Parent() interpreter.Activation {
	return nil
}

const (
	pick = "pick"
)

var (
	macros = celgo.Macros(
		parser.NewGlobalMacro("conditional", 3, conditionalExpander),
		parser.NewGlobalVarArgMacro(pick, pickExpander))
)

func conditionalExpander(eh parser.ExprHelper, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	if target != nil {
		return nil, &common.Error{
			Message:  "unexpected target in conditional",
			Location: eh.OffsetLocation(target.Id)}
	}

	// Convert conditional to a ternary operator.
	return eh.GlobalCall(operators.Conditional, args...), nil
}

func pickExpander(eh parser.ExprHelper, target *exprpb.Expr, args []*exprpb.Expr) (*exprpb.Expr, *common.Error) {
	if target != nil {
		return nil, &common.Error{
			Message:  "unexpected target in pick()",
			Location: eh.OffsetLocation(target.Id)}
	}

	out := args[len(args)-1]
	var selector *exprpb.Expr
	for i := len(args) - 2; i >= 0; i-- {
		selector = nil
		switch lhs := args[i].ExprKind.(type) {
		case *exprpb.Expr_SelectExpr:
			if lhs.SelectExpr.TestOnly {
				return nil, &common.Error{
					Message:  "unsupported use of has() macro in pick()",
					Location: eh.OffsetLocation(args[i].Id)}
			}
			// a.f | x --> has(a.f) ? a.f : x
			selector = eh.PresenceTest(lhs.SelectExpr.Operand, lhs.SelectExpr.Field)
		case *exprpb.Expr_CallExpr:
			if lhs.CallExpr.Function == operators.Index {
				// a["f"] | x --> "f" in a ? a["f"] : x
				selector = eh.GlobalCall(operators.In, lhs.CallExpr.Args[1], lhs.CallExpr.Args[0])
			}
		}

		// otherwise, a | b --> a
		if selector == nil {
			out = args[i]
		} else {
			out = eh.GlobalCall(operators.Conditional, selector, args[i], out)
		}
	}

	return out, nil
}
