// Copyright 2017 Istio Authors
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

package data

import (
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
)

func BuildTemplates(override *template.Info) map[string]*template.Info {
	var t = map[string]*template.Info{
		"t1": {
			Name:   "t1",
			CtrCfg: &types.Empty{},

			InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
				_, _ = evalFn("source.name")
				return &types.Empty{}, nil
			},
			BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
				return true
			},
			HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
				return true
			},
			SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {

			},
		},
	}

	if override != nil {
		if override.InferType != nil {
			t["t1"].InferType = override.InferType
		}
		if override.BuilderSupportsTemplate != nil {
			t["t1"].BuilderSupportsTemplate = override.BuilderSupportsTemplate
		}
		if override.HandlerSupportsTemplate != nil {
			t["t1"].HandlerSupportsTemplate = override.HandlerSupportsTemplate
		}
	}

	return t
}
