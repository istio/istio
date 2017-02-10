// Copyright 2016 Google Inc.
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

package adapterManager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/config"
	configpb "istio.io/mixer/pkg/config/proto"
)

func TestExecute(t *testing.T) {
	cases := []struct {
		name     string
		inCode   code.Code
		inErr    error
		wantCode code.Code
	}{
		{aspect.DenialsKindName, code.Code_OK, nil, code.Code_OK},
		{"error", code.Code_UNKNOWN, fmt.Errorf("expected"), code.Code_UNKNOWN},
	}

	for _, c := range cases {
		mngr := newTestManager(c.name, false, func() (*aspect.Output, error) {
			return &aspect.Output{Code: c.inCode}, c.inErr
		})
		mreg := map[aspect.Kind]aspect.Manager{
			aspect.DenialsKind: mngr,
		}
		breg := &fakeBuilderReg{
			adp:   mngr,
			found: true,
		}
		m := NewParallelManager(newManager(breg, mreg, nil, nil), 1)

		cfg := []*config.Combined{
			{&configpb.Adapter{Name: c.name}, &configpb.Aspect{Kind: c.name}},
		}

		o, err := m.Execute(context.Background(), cfg, nil, nil)
		if c.inErr != nil && err == nil {
			t.Errorf("m.Execute(...) = %v; want err: %v", err, c.inErr)
		}
		if c.inErr == nil && !(len(o) > 0 && o[0].Code == code.Code_OK) {
			t.Errorf("m.Execute(...) = %v; wanted len(o) == 1 && o[0].Code == code.Code_OK", o)
		}
	}
}

func TestExecute_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// we're skipping NewMethodHandlers so we don't have to deal with config since configuration should've matter when we have a canceled ctx
	handler := NewParallelManager(&Manager{}, 1)
	cancel()

	cfg := []*config.Combined{
		{&configpb.Adapter{Name: ""}, &configpb.Aspect{Kind: ""}},
	}
	if _, err := handler.Execute(ctx, cfg, &fakebag{}, nil); err == nil {
		t.Error("handler.Execute(canceledContext, ...) = _, nil; wanted any err")
	}

}

func TestExecute_TimeoutWaitingForResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	blockChan := make(chan struct{})

	name := "blocked"
	mngr := newTestManager(name, false, func() (*aspect.Output, error) {
		<-blockChan
		return &aspect.Output{Code: code.Code_OK}, nil
	})
	mreg := map[aspect.Kind]aspect.Manager{
		aspect.DenialsKind: mngr,
	}
	breg := &fakeBuilderReg{
		adp:   mngr,
		found: true,
	}
	m := NewParallelManager(newManager(breg, mreg, nil, nil), 1)

	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	cfg := []*config.Combined{{
		&configpb.Adapter{Name: name},
		&configpb.Aspect{Kind: name},
	}}
	if _, err := m.Execute(ctx, cfg, &fakebag{}, nil); err == nil {
		t.Error("handler.Execute(canceledContext, ...) = _, nil; wanted any err")
	}
	close(blockChan)
}

func TestShutdown(t *testing.T) {
	fail := make(chan struct{})
	succeed := make(chan struct{})
	p := NewParallelManager(&Manager{}, 1)

	go func() {
		time.Sleep(1 * time.Second)
		close(fail)
	}()

	go func() {
		p.Shutdown()
		close(succeed)
	}()

	select {
	case <-fail:
		t.Error("parallelManager.shutdown() didn't complete in the expected time")
	case <-succeed:
	}
}
