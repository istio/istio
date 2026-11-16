// Copyright Istio Authors
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

package xds_test

import (
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	xdsfake "istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestDebugGenRejectsMalformedResourceName(t *testing.T) {
	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
	gen := xds.NewDebugGen(s.Discovery, "istio-system", http.NewServeMux())

	for _, name := range []string{"%zz", "http://[::1", "config\x7fdump"} {
		for _, ns := range []string{"istio-system", "production"} {
			t.Run(ns+"/"+name, func(t *testing.T) {
				proxy := &model.Proxy{
					ID:               "test." + ns,
					VerifiedIdentity: &spiffe.Identity{Namespace: ns},
				}
				w := &model.WatchedResource{TypeUrl: v3.DebugType, ResourceNames: sets.New(name)}

				_, _, err := gen.Generate(proxy, w, nil)
				assert.Equal(t, status.Code(err), codes.InvalidArgument)

				_, _, _, _, err = gen.GenerateDeltas(proxy, nil, w)
				assert.Equal(t, status.Code(err), codes.InvalidArgument)
			})
		}
	}
}
