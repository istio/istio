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
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"

	mixerpb "istio.io/api/mixer/v1"
	istioconfig "istio.io/api/mixer/v1/config"

	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
)

func TestRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// we're skipping NewMethodHandlers so we don't have to deal with config since configuration should've matter when we have a canceled ctx
	handler := &methodHandlers{
		configs: map[Method][]*aspect.CombinedConfig{Check: {&aspect.CombinedConfig{}}},
	}

	cancel()

	s := handler.execute(ctx, attribute.NewManager().NewTracker(), &mixerpb.Attributes{}, Check)
	if s.Code != int32(code.Code_DEADLINE_EXCEEDED) {
		t.Errorf("execute(canceledContext, ...) returned %v, wanted status with code %v", s, code.Code_DEADLINE_EXCEEDED)
	}
}

func TestAspectManagerErrorsPropagated(t *testing.T) {
	// invalid configs so the aspectmanager.Manager fails
	handler := &methodHandlers{
		mngr:    adapterManager.NewManager(nil),
		configs: map[Method][]*aspect.CombinedConfig{Check: {&aspect.CombinedConfig{&istioconfig.Aspect{Kind: ""}, &istioconfig.Adapter{}}}},
	}

	s := handler.execute(context.Background(), attribute.NewManager().NewTracker(), &mixerpb.Attributes{}, Check)
	if s.Code != int32(code.Code_INTERNAL) {
		t.Errorf("execute(..., invalidConfig, ...) returned %v, wanted status with code %v", s, code.Code_INTERNAL)
	}
}
