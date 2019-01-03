// Copyright 2018 Istio Authors
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
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang"
)

func standardFunctions() []*exprpb.Decl {
	return []*exprpb.Decl{
		decls.NewFunction("startsWith",
			decls.NewInstanceOverload("startsWith",
				[]*exprpb.Type{decls.String, decls.String}, decls.Bool)),
		decls.NewFunction("endsWith",
			decls.NewInstanceOverload("endsWith",
				[]*exprpb.Type{decls.String, decls.String}, decls.Bool)),
		decls.NewFunction("match",
			decls.NewOverload("match",
				[]*exprpb.Type{decls.String, decls.String}, decls.Bool)),
		decls.NewFunction("reverse",
			decls.NewInstanceOverload("reverse",
				[]*exprpb.Type{decls.String}, decls.String)),
		decls.NewFunction("reverse",
			decls.NewOverload("reverse",
				[]*exprpb.Type{decls.String}, decls.String)),
		decls.NewFunction("toLower",
			decls.NewOverload("toLower",
				[]*exprpb.Type{decls.String}, decls.String)),
		decls.NewFunction("email",
			decls.NewOverload("email",
				[]*exprpb.Type{decls.String}, decls.NewObjectType(emailAddressType))),
		decls.NewFunction("dnsName",
			decls.NewOverload("dnsName",
				[]*exprpb.Type{decls.String}, decls.NewObjectType(dnsType))),
		decls.NewFunction("uri",
			decls.NewOverload("uri",
				[]*exprpb.Type{decls.String}, decls.NewObjectType(uriType))),
		decls.NewFunction("ip",
			decls.NewOverload("ip",
				[]*exprpb.Type{decls.String}, decls.NewObjectType(ipAddressType))),
		decls.NewFunction("emptyStringMap",
			decls.NewOverload("emptyStringMap",
				[]*exprpb.Type{}, stringMapType)),
	}
}

func standardOverloads() []*functions.Overload {
	return []*functions.Overload{
		{Operator: "startsWith",
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				if lhs.Type() != types.StringType || rhs.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to argument types")
				}
				return types.Bool(lang.ExternStartsWith(lhs.Value().(string), rhs.Value().(string)))
			}},
		{Operator: "endsWith",
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				if lhs.Type() != types.StringType || rhs.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to argument types")
				}
				return types.Bool(lang.ExternEndsWith(lhs.Value().(string), rhs.Value().(string)))
			}},
		{Operator: "match",
			Binary: func(lhs ref.Value, rhs ref.Value) ref.Value {
				if lhs.Type() != types.StringType || rhs.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to argument types")
				}
				return types.Bool(lang.ExternMatch(lhs.Value().(string), rhs.Value().(string)))
			}},
		{Operator: "reverse",
			Unary: func(v ref.Value) ref.Value {
				if v.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to '%s'", v.Type())
				}
				s := v.Value().(string)
				runes := []rune(s)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				return types.String(string(runes))
			}},
		{Operator: "toLower",
			Unary: func(v ref.Value) ref.Value {
				if v.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to '%s'", v.Type())
				}
				return types.String(lang.ExternToLower(v.Value().(string)))
			}},
		{Operator: "email",
			Unary: func(v ref.Value) ref.Value {
				if v.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to '%s'", v.Type())
				}
				out, err := lang.ExternEmail(v.Value().(string))
				if err != nil {
					return types.NewErr(err.Error())
				}
				return wrapperValue{typ: v1beta1.EMAIL_ADDRESS, s: out}
			}},
		{Operator: "dnsName",
			Unary: func(v ref.Value) ref.Value {
				if v.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to '%s'", v.Type())
				}
				out, err := lang.ExternDNSName(v.Value().(string))
				if err != nil {
					return types.NewErr(err.Error())
				}
				return wrapperValue{typ: v1beta1.DNS_NAME, s: out}
			}},
		{Operator: "uri",
			Unary: func(v ref.Value) ref.Value {
				if v.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to '%s'", v.Type())
				}
				out, err := lang.ExternURI(v.Value().(string))
				if err != nil {
					return types.NewErr(err.Error())
				}
				return wrapperValue{typ: v1beta1.URI, s: out}
			}},
		{Operator: "ip",
			Unary: func(v ref.Value) ref.Value {
				if v.Type() != types.StringType {
					return types.NewErr("overload cannot be applied to '%s'", v.Type())
				}
				out, err := lang.ExternIP(v.Value().(string))
				if err != nil {
					return types.NewErr(err.Error())
				}
				return wrapperValue{typ: v1beta1.IP_ADDRESS, bytes: out}
			}},
		{Operator: "emptyStringMap",
			Function: func(...ref.Value) ref.Value {
				return emptyStringMap
			}},
	}
}
