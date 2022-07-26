//go:build integ
// +build integ

/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package operator

import (
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

// operatorDumper dumps the logs of in-cluster operator at suite completion
type operatorDumper struct {
	ns  string
	rev string
}

func (d *operatorDumper) Dump(ctx resource.Context) {
	scopes.Framework.Errorf("=== Dumping Istio Deployment State for %v...", ctx.ID())
	ns := d.ns
	if len(ns) < 1 {
		ns = "istio-operator"
	}

	dir, err := ctx.CreateTmpDirectory("istio-operator-" + d.ID().String())
	if err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping operator contents: %v", err)
		return
	}
	kube2.DumpPods(ctx, dir, ns, []string{"name=istio-operator"})
}

func (d *operatorDumper) ID() resource.ID {
	return &operatorID{d.rev}
}

type operatorID struct {
	content string
}

func (o *operatorID) String() string {
	return o.content
}
