//  Copyright 2019 Istio Authors
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

package sdsmanagedcaflow

import (
	"fmt"
	"testing"

	"istio.io/pkg/env"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	inst istio.Instance
	g    galley.Instance
	p    pilot.Instance
)

// Cluster and project specific values from env vars.
var (
	projectID       string
	clusterLocation string
	clusterName     string
)

func TestMain(m *testing.M) {
	// NOTE: Currently, this test is only intended to run manually on a managed CA whitelisted cluster.
	projectID = env.RegisterStringVar("PROJECT_ID", "", "").Get()
	clusterLocation = env.RegisterStringVar("CLUSTER_LOCATION", "", "").Get()
	clusterName = env.RegisterStringVar("CLUSTER_NAME", "", "").Get()
	if projectID == "" || clusterLocation == "" || clusterName == "" {
		return
	}
	framework.
		NewSuite("sds_managed_ca_flow_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// Overwrite Istio standard helm values with managed CA and IDNS.
	cfg.ValuesFile = "example-values/values-istio-googleca.yaml"
	cfg.Values["global.trustDomain"] = fmt.Sprintf("%s.svc.id.goog", projectID)
	cfg.Values["nodeagent.env.GKE_CLUSTER_URL"] = fmt.Sprintf(
		"https://container.googleapis.com/v1/projects/%s/locations/%s/clusters/%s",
		projectID, clusterLocation, clusterName)
}
