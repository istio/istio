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

// Package showcase contains a test suite implementation example that showcases a particular/opinionated
// design sketch.
package showcase

import (
	"testing"

	"istio.io/istio/pkg/test"
	//"istio.io/istio/pkg/test/charts"
	//"istio.io/istio/pkg/test/cluster"
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/label"
)

// Capturing TestMain allows us to:
// - Do cleanup before exit
// - process testing specific flags
func TestMain(m *testing.M) {
	test.Run("showcase_test", m)
}

// Ignore this test with a reason. Uses Go's "Skip"
func TestIgnored(t *testing.T) {
	test.Ignore(t, "bad test")
}

// Test categorization can be used to filter out tests.
// Try running "go test -labels=integration" or "go test -labels=Pilot" etc.
func TestCategorization(t *testing.T) {
	test.Tag(t, label.Integration, label.Pilot, label.Mixer)
}

// Requirement checks and ensures that the specified requirement can be satisfied.
// In this case, the "APIServer" dependency will be initialized (once per running suite) and be available
// for use.
func TestRequirement_Cluster(t *testing.T) {
	test.Requires(t, dependency.Cluster)
}

// A requirement that will cause failure on Mac. Tags can be used to filter out as well.
func TestRequirement_UnsatisfiedButSkipped(t *testing.T) {
	test.Tag(t, label.LinuxOnly) // LinuxOnly is a special flag that causes the test to be skipped on non-Linux environments.
}

// Showcase using local components only, without a cluster.
func TestDeployment_Local(t *testing.T) {
	test.Tag(t, label.Integration)
	test.Requires(t, dependency.Mixer, dependency.Pilot)

	e := test.GetEnvironment(t)
	e.Configure(`aaa`)

	m := e.GetMixer()
	_ = m.Report(nil)
}

func TestDeployment_Helm(t *testing.T) {
	test.Tag(t, label.Integration)
	test.Requires(t, dependency.Cluster) // Specify that we need *a* Cluster (can be Minikube, GKE, K8s etc.)

	e := test.GetEnvironment(t)
	//e.Deploy(charts.Istio) // Deploy a shrink-wrapped Helm chart to the cluster.

	a := e.GetAPIServer()                             // Get the singleton API Server
	g := e.GetIstioComponent(test.GalleyComponent)[0] // Get a list of Galley instances and pick the first one.

	//cfg := a.Config() // Kube config to access the API Server directly.

	_ = g
	_ = a
	//_ = cfg
}

func TestRequirement_ExclusiveCluster(t *testing.T) {
	test.Tag(t, label.Integration)
	test.Requires(t, dependency.ExclusiveCluster) // We need a cluster with exclusive access

	// ....
}

func TestRequirement_GKE(t *testing.T) {
	test.Tag(t, label.Integration)
	test.Requires(t, dependency.GKE) // We specifically need GKE

	// ....
}
