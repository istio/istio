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

package repair

import (
	"context"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/pkg/kube"
)

var repairLog = scopes.CNIAgent

func StartRepair(ctx context.Context, cfg config.RepairConfig) {
	if !cfg.Enabled {
		repairLog.Info("CNI repair controller is disabled")
		return
	}
	repairLog.Info("starting CNI sidecar repair controller")

	client, err := clientSetup()
	if err != nil {
		repairLog.Fatalf("CNI repair could not construct clientSet: %s", err)
	}

	rc, err := NewRepairController(client, cfg)
	if err != nil {
		repairLog.Fatalf("Fatal error constructing repair controller: %+v", err)
	}
	go rc.Run(ctx.Done())
	client.RunAndWait(ctx.Done())
}

// Set up Kubernetes client using kubeconfig (or in-cluster config if no file provided)
func clientSetup() (kube.Client, error) {
	config, err := kube.DefaultRestConfig("", "")
	if err != nil {
		return nil, err
	}
	return kube.NewClient(kube.NewClientConfigForRestConfig(config), "")
}
