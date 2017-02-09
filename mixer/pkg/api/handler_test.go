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

package api

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
)

type fakeresolver struct {
	ret []*config.Combined
	err error
}

func (f *fakeresolver) Resolve(bag attribute.Bag, aspectSet config.AspectSet) ([]*config.Combined, error) {
	return f.ret, f.err
}

type fakeExecutor struct {
	body func() (*aspect.Output, error)
}

// BulkExecute takes a set of configurations and Executes all of them.
func (f *fakeExecutor) Execute(ctx context.Context, cfgs []*config.Combined, attrs attribute.Bag) ([]*aspect.Output, error) {
	o, err := f.body()
	if err != nil {
		return nil, err
	}
	return []*aspect.Output{o}, nil
}

func TestAspectManagerErrorsPropagated(t *testing.T) {
	f := &fakeExecutor{func() (*aspect.Output, error) {
		return nil, fmt.Errorf("expected")
	}}
	h := &handlerState{aspectExecutor: f, methodmap: map[config.APIMethod]config.AspectSet{}}
	h.ConfigChange(&fakeresolver{[]*config.Combined{nil, nil}, nil})

	s := h.execute(context.Background(), attribute.NewManager().NewTracker(), &mixerpb.Attributes{}, config.CheckMethod)
	if s.Code != int32(code.Code_INTERNAL) {
		t.Errorf("execute(..., invalidConfig, ...) returned %v, wanted status with code %v", s, code.Code_INTERNAL)
	}
}
