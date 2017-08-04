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

package evaluator

import (
	"testing"

	pbv "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	iltesting "istio.io/mixer/pkg/il/testing"
)

func TestEval(t *testing.T) {
	e := initEvaluator(t, configInt)
	bag := initBag(int64(23))
	r, err := e.Eval("attr", bag)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	if r != int64(23) {
		t.Fatalf("Unexpected result: r:%v, expected: %v", r, 23)
	}
}

func TestEval_Error(t *testing.T) {
	e := initEvaluator(t, configInt)
	bag := initBag(int64(23))
	_, err := e.Eval("foo", bag)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestEvalString(t *testing.T) {
	e := initEvaluator(t, configString)
	bag := initBag("foo")
	r, err := e.EvalString("attr", bag)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	if r != "foo" {
		t.Fatalf("Unexpected result: r: %v, expected: %v", r, "foo")
	}
}

func TestEvalString_Error(t *testing.T) {
	e := initEvaluator(t, configString)
	bag := initBag("foo")
	_, err := e.EvalString("bar", bag)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestEvalString_DifferentType(t *testing.T) {
	e := initEvaluator(t, configInt)
	bag := initBag(int64(23))
	r, err := e.EvalString("attr", bag)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	if r != "23" {
		t.Fatalf("Unexpected result: r: %v, expected: %v", r, "23")
	}
}

func TestEvalPredicate(t *testing.T) {
	e := initEvaluator(t, configBool)
	bag := initBag(true)
	r, err := e.EvalPredicate("attr", bag)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	if !r {
		t.Fatal("Expected result to be true.")
	}
}

func TestEvalPredicate_Error(t *testing.T) {
	e := initEvaluator(t, configBool)
	bag := initBag(true)
	_, err := e.EvalPredicate("boo", bag)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestEvalPredicate_WrongType(t *testing.T) {
	e := initEvaluator(t, configBool)
	bag := initBag(int64(23))
	_, err := e.EvalPredicate("attr", bag)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestEvalType(t *testing.T) {
	e := initEvaluator(t, configBool)
	ty, err := e.EvalType("attr", e.finder)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	if ty != pbv.BOOL {
		t.Fatalf("Unexpected type: %v", ty)
	}
}

func TestEvalType_WrongType(t *testing.T) {
	e := initEvaluator(t, configBool)
	_, err := e.EvalType("boo", e.finder)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestAssertType(t *testing.T) {
	e := initEvaluator(t, configBool)
	err := e.AssertType("attr", e.finder, pbv.BOOL)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
}

func TestAssertType_WrongType(t *testing.T) {
	e := initEvaluator(t, configBool)
	err := e.AssertType("attr", e.finder, pbv.STRING)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestAssertType_EvaluationError(t *testing.T) {
	e := initEvaluator(t, configBool)
	err := e.AssertType("boo", e.finder, pbv.BOOL)
	if err == nil {
		t.Fatal("Was expecting an error")
	}
}

func TestConfigChange(t *testing.T) {
	e := initEvaluator(t, configInt)
	bag := initBag(int64(23))

	// Prime the cache
	_, err := e.evalResult("attr", bag)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	f := descriptor.NewFinder(&configBool)
	e.ConfigChange(nil, f, nil)
	if e.finder != f {
		t.Fatal("Finder is not set correctly")
	}

	bag = initBag(true)
	_, err = e.EvalPredicate("attr", bag)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func initBag(attrValue interface{}) attribute.Bag {
	attrs := make(map[string]interface{})
	attrs["attr"] = attrValue

	return &iltesting.FakeBag{Attrs: attrs}
}

func initEvaluator(t *testing.T, config pb.GlobalConfig) *IL {
	e, err := NewILEvaluator(10)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	finder := descriptor.NewFinder(&config)
	e.ConfigChange(nil, finder, nil)
	return e
}

var configInt = pb.GlobalConfig{
	Manifests: []*pb.AttributeManifest{
		{
			Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
				"attr": {
					ValueType: pbv.INT64,
				},
			},
		},
	},
}

var configString = pb.GlobalConfig{
	Manifests: []*pb.AttributeManifest{
		{
			Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
				"attr": {
					ValueType: pbv.STRING,
				},
			},
		},
	},
}

var configBool = pb.GlobalConfig{
	Manifests: []*pb.AttributeManifest{
		{
			Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
				"attr": {
					ValueType: pbv.BOOL,
				},
			},
		},
	},
}
