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

func TestRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// we're skipping NewMethodHandlers so we don't have to deal with config since configuration should've matter when we have a canceled ctx
	handler := &handlerState{}
	handler.ConfigChange(&fakeresolver{[]*config.Combined{nil, nil}, nil})
	cancel()

	s := handler.execute(ctx, attribute.NewManager().NewTracker(), &mixerpb.Attributes{}, config.CheckMethod)
	if s.Code != int32(code.Code_DEADLINE_EXCEEDED) {
		t.Errorf("execute(canceledContext, ...) got: %s [%v], want: %v", code.Code_name[s.Code], s, code.Code_DEADLINE_EXCEEDED)
	}
}
