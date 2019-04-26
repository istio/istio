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
//
// How to run this test suite locally:
// vmconfig="{
// 	'vm_namespace': 'default',
// 	'project_number': '895429144602',
// 	'project_id': 'jianfeih-test',
// 	'gcp_vm_zone': 'us-central1-a',
// 	'gke_cluster_name': 'istio-dev'
// }"
// go test -v ./tests/integration/meshexp
// -istio.test.env  kube -istio.test.hub "gcr.io/istio-release" \
// -istio.test.tag "master-latest-daily" \
// --log_output_level=tf:debug,CI:debug \
// -istio.test.kube.meshexp.debianUrl=https://storage.googleapis.com/istio-release/releases/1.1.3/deb  \
// -istio.test.kube.meshexp.vmconfig=$vmconfig
// -istio.test.nocleanup
//
// Package meshexp contains test suite for mesh expansion.
package meshexp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	old_framework "istio.io/istio/tests/e2e/framework"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/rawvm"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	vmInstance rawvm.Instance
	// ports is the port the VM service exposes.
	ports = model.PortList{
		&model.Port{
			Name:     "http",
			Port:     8080,
			Protocol: model.ProtocolHTTP,
		},
	}
)

const (
	serviceName = ""
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("meshexp_test", m).
		// Restrict the test to the K8s environment only, tests will be skipped in native environment.
		RequireEnvironment(environment.Kube).
		// Deploy Istio on the cluster.
		Setup(istio.SetupOnKube(nil, setupMeshExpansionInstall)).
		Run()
}

func setupMeshExpansionInstall(cfg *istio.Config) {
	cfg.Values["global.meshExpansion.enabled"] = "true"
}

// setupVMInstance runs necessary setup on the VM instance and create service
// entry for VM application.
func setupVMInstance(ctx resource.Context) error {
	var err error
	defaultConfig, err := istio.DefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to access default config, %v", err)
	}
	gceConfig := old_framework.GCPVMOpts{
		DebianURL: defaultConfig.MeshExpansionDebianURL,
	}
	if err := json.Unmarshal([]byte(defaultConfig.MeshExpansionConfig), &gceConfig); err != nil {
		return fmt.Errorf("failed to parse the config, config str %v, error %v",
			defaultConfig.MeshExpansionConfig, err)
	}
	vmInstance, err = rawvm.New(ctx, rawvm.Config{
		Type:        rawvm.GCE,
		GCPVMConfig: gceConfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create VM service %v", err)
	}
	rawvm.Register(serviceName, ports)
	return nil
}

func TestIstioControlPlaneReachability(t *testing.T) {
	testContext := framework.NewContext(t)
	defer testContext.Done(t)
	// Create a VM instance before running the test.
	// We do this setup in Test method instead of suite since the suite setup can't be supported on
	// some environments yet, for example, circleci.
	if err := setupVMInstance(testContext); err != nil {
		t.Errorf("failed to setup VM instance: %v", err)
	}
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Retry several times to reduce the flakes.
			output := ""
			var err error
			for i := 0; i < 10; i++ {
				// TODO(incfly): change to config_dump and convert to xDS proto might be better.
				output, err = vmInstance.Execute(`/bin/sh -c 'curl localhost:15000/clusters'`)
				if err == nil && output != "" {
					log.Infof("succussfully get envoy config")
					break
				}
				if err != nil {
					log.Errorf("[Attempt %v] VM instance failed to get Envoy CDS, %v\n", i, err)
				}
				time.Sleep(time.Second * 5)
			}
			if output == "" {
				t.Errorf("failed to get Envoy cluster config")
			}
			// Examine sidecar CDS config to see if control plane exists or not.
			for _, cluster := range []string{
				"istio-pilot.istio-system.svc.cluster.local",
				"istio-citadel.istio-system.svc.cluster.local",
			} {
				if !strings.Contains(output, cluster) {
					t.Errorf("%v not found in VM sidecar CDS config", cluster)
				}
			}
			// fmt.Println("jianfeih debugging... sleep")
			// time.Sleep(time.Second * 3600)
		})
}

// TestKubernetesToVM sends a request to a pod in Kubernetes cluster, then the pod sends the request
// to app runs on the VM, returns success if VM app returns success result.
// TODO(incfly): implemets it.
// func TestKubernetesToVM(t *testing.T) {
// }
