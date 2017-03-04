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

package api

import (
	"context"
	"flag"
	"fmt"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/status"
)

type fakeresolver struct {
	ret []*config.Combined
	err error
}

func (f *fakeresolver) Resolve(bag attribute.Bag, aspectSet config.AspectSet) ([]*config.Combined, error) {
	return f.ret, f.err
}

type fakeExecutor struct {
	body func() aspect.Output
}

// Execute takes a set of configurations and Executes all of them.
func (f *fakeExecutor) Execute(ctx context.Context, cfgs []*config.Combined, attrs attribute.Bag, ma aspect.APIMethodArgs) aspect.Output {
	return f.body()
}

func TestAspectManagerErrorsPropagated(t *testing.T) {
	f := &fakeExecutor{func() aspect.Output {
		return aspect.Output{Status: status.WithError(fmt.Errorf("expected"))}
	}}
	h := &handlerState{aspectExecutor: f, methodMap: map[aspect.APIMethod]config.AspectSet{}}
	h.ConfigChange(&fakeresolver{[]*config.Combined{nil, nil}, nil})

	s := h.execute(context.Background(), attribute.NewManager().NewTracker(), &mixerpb.Attributes{}, aspect.CheckMethod, nil)
	if s.Code != int32(rpc.INTERNAL) {
		t.Errorf("execute(..., invalidConfig, ...) returned %v, wanted status with code %v", s, rpc.INTERNAL)
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
