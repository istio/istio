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

package kubernetes

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/model"
)

// TODO(nmittler): Remove from final code. This is just helpful for initial debugging.

// These methods will only work if you already have an environment deployed with apps.
func TestHttp(t *testing.T) {
	e := Environment{
		TestNamespace: "istio-system",
	}

	a := e.GetAppOrFail("a", t)
	b := e.GetAppOrFail("b", t)

	endpoint := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	u := endpoint.MakeURL()
	u.Path = a.Name()

	result := a.CallOrFail(u, 1, nil, t)
	if !result.IsSuccess() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}
	spew.Dump(result)
}
