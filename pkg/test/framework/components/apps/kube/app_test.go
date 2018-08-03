//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube_test

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework/components/apps/kube"
	"istio.io/istio/pkg/test/framework/environment"
)

// TODO(nmittler): Remove from final code. This is just helpful for initial debugging.

const namespace = "istio-system"

func TestLocal(t *testing.T) {
	t.Skip("Skipping this test. Must be enabled manually.")

	a, err := kube.NewApp("a", namespace)
	if err != nil {
		t.Fatal(err)
	}
	b, err := kube.NewApp("b", namespace)
	if err != nil {
		t.Fatal(err)
	}

	endpoint := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	results := a.CallOrFail(endpoint, environment.AppCallOptions{}, t)
	for _, result := range results {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}
	spew.Dump(results)
}
