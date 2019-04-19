// Copyright 2019 Istio Authors
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

package apps_test

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

func TestNative(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{
		Galley: g,
	})

	ns := namespace.NewOrFail(t, ctx, "test", true)
	all := apps.NewOrFail(t, ctx, apps.Config{
		Namespace: ns,
		Galley:    g,
		Pilot:     p,
		AppParams: []apps.AppParam{
			{
				Name: "a",
			},
			{
				Name: "b",
			},
		},
	})
	a := all.GetAppOrFail("a", t)
	b := all.GetAppOrFail("b", t)

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
	_ = a.CallOrFail(be, apps.AppCallOptions{}, t)
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("apps_test", m).
		RequireEnvironment(environment.Native).
		Run()
}
