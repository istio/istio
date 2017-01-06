// Copyright 2017 Google Inc.
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

package logger

import (
	"fmt"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/genproto/googleapis/rpc/code"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/aspect/logger"
	"istio.io/mixer/pkg/aspectsupport"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

type (
	manager struct{}

	executor struct {
		inputs map[string]string
		aspect logger.Aspect
	}
)

// NewManager returns an aspect manager for the logger aspect.
func NewManager() aspectsupport.Manager {
	return &manager{}
}

func (m *manager) NewAspect(c *aspectsupport.CombinedConfig, a aspect.Adapter, env aspect.Env) (aspectsupport.AspectWrapper, error) {
	// cast to logger.Adapter from aspect.Adapter
	logAdapter, ok := a.(logger.Adapter)
	if !ok {
		return nil, fmt.Errorf("adapter of incorrect type. Expected logger.Adapter got %#v %T", a, a)
	}

	// TODO: replace when c.Adapter.TypedParams is ready
	cpb := logAdapter.DefaultConfig()
	if c.Adapter.Params != nil {
		if err := structToProto(c.Adapter.Params, cpb); err != nil {
			return nil, fmt.Errorf("could not parse adapter config: %v", err)
		}
	}

	aspectImpl, err := logAdapter.NewAspect(env, cpb)
	if err != nil {
		return nil, err
	}

	var inputs map[string]string
	if c.Aspect != nil && c.Aspect.Inputs != nil {
		inputs = c.Aspect.Inputs
	}

	return &executor{inputs, aspectImpl}, nil
}

func (*manager) Kind() string                                                      { return "istio/logger" }
func (*manager) DefaultConfig() proto.Message                                      { return &empty.Empty{} }
func (*manager) ValidateConfig(implConfig proto.Message) (ce *aspect.ConfigErrors) { return }

func (e *executor) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspectsupport.Output, error) {
	entry := make(logger.Entry)
	for attr, expr := range e.inputs {
		if val, err := mapper.Eval(expr, attrs); err == nil {
			entry[attr] = val
		}
		// TODO: define error-handling bits (is mapping failure a
		//       total failure, or should we just not pass the attr
		//       and let adapter impls decide if that is an error
		//       condition?)
	}
	if err := e.aspect.Log([]logger.Entry{entry}); err != nil {
		return nil, err
	}
	return &aspectsupport.Output{code.Code_OK}, nil
}

func structToProto(in *structpb.Struct, out proto.Message) error {
	mm := &jsonpb.Marshaler{}
	str, err := mm.MarshalToString(in)
	if err != nil {
		return fmt.Errorf("failed to marshal to string: %v", err)
	}
	return jsonpb.UnmarshalString(str, out)
}
