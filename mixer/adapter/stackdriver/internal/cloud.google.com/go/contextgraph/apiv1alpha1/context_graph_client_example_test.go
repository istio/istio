// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// AUTO-GENERATED CODE. DO NOT EDIT.

package contextgraph_test

import (
	"golang.org/x/net/context"

	contextgraph "istio.io/istio/mixer/adapter/stackdriver/internal/cloud.google.com/go/contextgraph/apiv1alpha1"
	contextgraphpb "istio.io/istio/mixer/adapter/stackdriver/internal/google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
)

func ExampleNewClient() {
	ctx := context.Background()
	c, err := contextgraph.NewClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}
	// TODO: Use client.
	_ = c
}

func ExampleClient_AssertBatch() {
	ctx := context.Background()
	c, err := contextgraph.NewClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}

	req := &contextgraphpb.AssertBatchRequest{
		// TODO: Fill request struct fields.
	}
	resp, err := c.AssertBatch(ctx, req)
	if err != nil {
		// TODO: Handle error.
	}
	// TODO: Use resp.
	_ = resp
}
