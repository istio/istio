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

package config

import (
	"fmt"
	"testing"

	"github.com/hashicorp/go-multierror"
	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
	pb "istio.io/mixer/pkg/config/proto"
)

type trueEval struct {
	err    error
	ncalls int
	ret    bool
}

func (t *trueEval) EvalPredicate(expression string, attrs attribute.Bag) (bool, error) {
	if t.ncalls == 0 {
		return t.ret, t.err
	}
	t.ncalls--
	return true, nil
}

type ttable struct {
	err    error
	ncalls int
	ret    bool
	nlen   int
	asp    []string
}

func TestRuntime(t *testing.T) {
	table := []*ttable{
		{nil, 0, true, 4, []string{"listChecker"}},
		{nil, 1, false, 2, []string{"listChecker"}},
		{fmt.Errorf("predicate error"), 1, false, 2, []string{"listChecker"}},
		{nil, 0, true, 0, []string{}},
		{fmt.Errorf("predicate error"), 0, true, 0, []string{"listChecker"}},
	}

	LC := "listChecker"
	a1 := &pb.Adapter{
		Name: "a1",
		Kind: LC,
	}
	a2 := &pb.Adapter{
		Name: "a2",
		Kind: LC,
	}

	v := &Validated{
		adapterByName: map[adapterKey]*pb.Adapter{
			adapterKey{LC, "a1"}: a1,
			adapterKey{LC, "a2"}: a2,
		},
		serviceConfig: &pb.ServiceConfig{
			Rules: []*pb.AspectRule{
				{
					Selector: "ok",
					Aspects: []*pb.Aspect{
						{
							Kind: LC,
						},
						{
							Adapter: "a2",
							Kind:    LC,
						},
					},
					Rules: []*pb.AspectRule{
						{
							Selector: "ok",
							Aspects: []*pb.Aspect{
								{
									Kind: LC,
								},
								{
									Adapter: "a2",
									Kind:    LC,
								},
							},
						},
					},
				},
			},
		},
		numAspects: 1,
	}

	bag, err := attribute.NewManager().NewTracker().StartRequest(&mixerpb.Attributes{})
	if err != nil {
		t.Error("Unable to get attribute bag")
	}

	for idx, tt := range table {
		fe := &trueEval{tt.err, tt.ncalls, tt.ret}
		aspects := make(map[string]bool)
		for _, a := range tt.asp {
			aspects[a] = true
		}
		rt := NewRuntime(v, fe)

		al, err := rt.Resolve(bag, aspects)

		if tt.err != nil {
			merr := err.(*multierror.Error)
			if merr.Errors[0] != tt.err {
				t.Error(idx, "expected:", tt.err, "\ngot:", merr.Errors[0])
			}
		}

		if len(al) != tt.nlen {
			t.Errorf("%d Expected %d resolve got %d", idx, tt.nlen, len(al))
		}
	}
}
