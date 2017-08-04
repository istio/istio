// Copyright 2016 Istio Authors
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

package aspect

import (
	"errors"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/config"
	cpb "istio.io/mixer/pkg/config/proto"
)

func TestDenialsManager(t *testing.T) {
	dm := newDenialsManager()
	if dm.Kind() != config.DenialsKind {
		t.Errorf("m.Kind() = %s wanted %s", dm.Kind(), config.DenialsKind)
	}
	if err := dm.ValidateConfig(dm.DefaultConfig(), nil, nil); err != nil {
		t.Errorf("ValidateConfig(DefaultConfig()) produced an error: %v", err)
	}
}

type testBuilder struct {
	adapter.DefaultBuilder
	returnErr bool
}

func newBuilder(returnErr bool) testBuilder {
	return testBuilder{adapter.NewDefaultBuilder("test", "test", nil), returnErr}
}

func (t testBuilder) NewDenialsAspect(env adapter.Env, c adapter.Config) (adapter.DenialsAspect, error) {
	if t.returnErr {
		return nil, errors.New("error")
	}
	return &testDenier{}, nil
}

func TestDenialsManager_NewCheckExecutor(t *testing.T) {
	defaultCfg := &cpb.Combined{
		Builder: &cpb.Adapter{Params: &aconfig.DenialsParams{}},
	}

	dm := newDenialsManager()
	f, _ := FromBuilder(newBuilder(false), config.DenialsKind)
	if _, err := dm.NewCheckExecutor(defaultCfg, f, test.Env{}, nil, ""); err != nil {
		t.Errorf("NewCheckExecutor() returned an unexpected error: %v", err)
	}
}

func TestDenialsManager_NewCheckExecutorErrors(t *testing.T) {
	defaultCfg := &cpb.Combined{
		Builder: &cpb.Adapter{Params: &aconfig.DenialsParams{}},
	}

	dm := newDenialsManager()
	f, _ := FromBuilder(newBuilder(true), config.DenialsKind)
	if _, err := dm.NewCheckExecutor(defaultCfg, f, test.Env{}, nil, ""); err == nil {
		t.Error("NewCheckExecutor() should have propogated error.")
	}
}

type testDenier struct {
	adapter.Aspect
	closed bool
}

func (d *testDenier) Close() error {
	d.closed = true
	return nil
}

func (d *testDenier) Deny() rpc.Status {
	return rpc.Status{Code: int32(rpc.PERMISSION_DENIED)}
}

func TestDenialsExecutor_Execute(t *testing.T) {
	executor := &denialsExecutor{&testDenier{}}

	got := executor.Execute(test.NewBag(), test.NewIDEval())
	if got.Code != int32(rpc.PERMISSION_DENIED) {
		t.Errorf("Execute() => %v, wanted %v", got.Code, rpc.PERMISSION_DENIED)
	}
}

func TestDenialsExecutor_Close(t *testing.T) {
	inner := &testDenier{closed: false}
	executor := &denialsExecutor{inner}
	if err := executor.Close(); err != nil {
		t.Errorf("Close() returned an error: %v", err)
	}
	if !inner.closed {
		t.Error("Close() should propagate to wrapped aspect.")
	}
}
