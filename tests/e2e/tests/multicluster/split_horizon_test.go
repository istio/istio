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

package multicluster

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/multierr"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	defaultRetryBudget             = 10
	retryDelay                     = time.Second
	istioIngressGatewayServiceName = "istio-ingressgateway"
	istioIngressGatewayLabel       = "ingressgateway"
	istioMeshConfigMapName         = "istio"
	defaultPropagationDelay        = 5 * time.Second
	maxDeploymentTimeout           = 480 * time.Second
	primaryCluster                 = framework.PrimaryCluster
	remoteCluster                  = framework.RemoteCluster
	clusterAwareGateway            = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: cluster-aware-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 443
      name: tls
      protocol: TLS
    tls:
      mode: AUTO_PASSTHROUGH
    hosts:
    - "*.local"
`
)

var (
	tc = &testConfig{}
)

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitLogging(), "cannot setup logging")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}

func TestRemoteInstanceAccessible(t *testing.T) {
	// Get the sleep pod name
	podName, err := util.GetPodName("sample", "app=sleep", tc.Kube.KubeConfig)
	if err != nil {
		t.Fatalf("couldn't get the sleep pod name. Error: %s", err.Error())
	}

	// Out of the 10 tries no response from remote cluster therefore fail the test
	runRetriableTest(t, "GetResponseFromRemote", defaultRetryBudget, func() error {
		output, err := util.PodExec("sample", podName, "sleep", "curl helloworld.sample:5000/hello", false, tc.Kube.KubeConfig)
		if err != nil {
			t.Fatalf("couldn't execute a curl call from the sleep pod. Error: %s", err.Error())
		}

		// Check whether the response is from remote cluster (v2). If it is then the
		// test is successful
		if strings.Contains(output, "Hello version: v2") {
			log.Info("got response from the helloworld v2 instance (remote cluster)")
			return nil
		}
		return fmt.Errorf("received only responses from primary (v1) while expecting at least one response from remote (v2)")
	})
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("split_horizon_test")
	if err != nil {
		return err
	}
	tc.CommonConfig = cc

	if tc.Kube.RemoteKubeConfig == "" {
		return fmt.Errorf("no RemoteKubeConfig")
	}

	appDir, err := ioutil.TempDir(os.TempDir(), "split_horizon_test")
	if err != nil {
		return err
	}
	tc.AppDir = appDir

	// Extra system configuration required for the pilot tests.
	tc.extraConfig = make(map[string]*deployableConfig)

	// Deplyment configration for the primary cluster
	if kc, ok := tc.Kube.Clusters[primaryCluster]; ok {
		tc.extraConfig[primaryCluster] = &deployableConfig{
			Namespace:      "sample",
			IstioInjection: true,
			YamlFiles: []yamlFile{
				{
					// Select the service from the yaml file
					Name:         "testdata/helloworld.yaml",
					LabelSeletor: "app=helloworld",
				},
				{
					// Select the v1 deployment from the yaml file
					Name:         "testdata/helloworld.yaml",
					LabelSeletor: "version=v1",
				},
				{
					// sleep service yaml file
					Name: "testdata/sleep.yaml",
				},
			},
			kubeconfig: kc,
		}
	}

	if kc, ok := tc.Kube.Clusters[remoteCluster]; ok {
		tc.extraConfig[remoteCluster] = &deployableConfig{
			Namespace:      "sample",
			IstioInjection: true,
			YamlFiles: []yamlFile{
				{
					// Select the service from the yaml file
					Name:         "testdata/helloworld.yaml",
					LabelSeletor: "app=helloworld",
				},
				{
					// Select the v2 deployment from the yaml file
					Name:         "testdata/helloworld.yaml",
					LabelSeletor: "version=v2",
				},
			},
			kubeconfig: kc,
		}
	}

	return nil
}

func check(err error, msg string) {
	if err != nil {
		log.Errorf("%s. Error %s", msg, err)
		os.Exit(-1)
	}
}

// runRetriableTest runs the given test function the provided number of times.
func runRetriableTest(t *testing.T, testName string, retries int, f func() error, errorFunc ...func()) {
	t.Run(testName, func(t *testing.T) {
		remaining := retries
		for {
			// Call the test function.
			remaining--
			err := f()
			if err == nil {
				// Test succeeded, we're done here.
				return
			}

			if remaining == 0 {
				// We're out of retries - fail the test now.
				for _, e := range errorFunc {
					if e != nil {
						e()
					}
				}
				t.Fatal(err)
			}

			// Wait for a bit before retrying.
			retries--
			time.Sleep(retryDelay)
		}
	})
}

type yamlFile struct {
	Name         string
	LabelSeletor string
}
type resource struct {
	// Kind of the resource
	Kind string
	// Name of the resource
	Name string
}

// deployableConfig is a collection of configs that are applied/deleted as a single unit.
type deployableConfig struct {
	Namespace      string
	IstioInjection bool
	YamlFiles      []yamlFile

	// List of resources must be removed during deployableConfig setup, and restored
	// during teardown. These resources must exist before deployableConfig setup runs, and should be
	// in the same namespace defined above. Typically, they are added by the default Istio installation
	// (e.g the default global authentication policy) and need to be modified for tests.
	Removes    []resource
	applied    []string
	removed    []string
	kubeconfig string
}

// Setup pushes the config and waits for it to propagate to all nodes in the cluster.
func (c *deployableConfig) Setup() error {
	c.removed = []string{}
	for _, r := range c.Removes {
		content, err := util.KubeGetYaml(c.Namespace, r.Kind, r.Name, c.kubeconfig)
		if err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		if err := util.KubeDeleteContents(c.Namespace, content, c.kubeconfig); err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		c.removed = append(c.removed, content)
	}

	if c.Namespace != tc.Kube.Namespace {
		if err := util.CreateNamespace(c.Namespace, c.kubeconfig); err != nil {
			_ = c.Teardown()
			return err
		}
	}

	if c.IstioInjection {
		if err := util.LabelNamespace(c.Namespace, "istio-injection=enabled", c.kubeconfig); err != nil {
			_ = c.Teardown()
			return err
		}
	}

	c.applied = []string{}
	// Apply the yaml file(s)
	for _, yamlFile := range c.YamlFiles {
		kubeCmd := "apply"
		if len(yamlFile.LabelSeletor) > 0 {
			kubeCmd += " -l " + yamlFile.LabelSeletor
		}
		if err := util.KubeCommand(kubeCmd, c.Namespace, yamlFile.Name, c.kubeconfig); err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		c.applied = append(c.applied, yamlFile.Name)
	}

	// Wait until the deployments are ready
	if err := util.CheckDeployments(c.Namespace, maxDeploymentTimeout, c.kubeconfig); err != nil {
		_ = c.Teardown()
		return err
	}

	return nil
}

// Teardown deletes the deployed configuration.
func (c *deployableConfig) Teardown() error {
	err := c.TeardownNoDelay()

	// Sleep for a while to allow the change to propagate.
	time.Sleep(c.propagationDelay())
	return err
}

// Teardown deletes the deployed configuration.
func (c *deployableConfig) TeardownNoDelay() error {
	var err error
	for _, yamlFile := range c.applied {
		err = multierr.Append(err, util.KubeDelete(c.Namespace, yamlFile, c.kubeconfig))
	}
	// Restore configs that was removed
	for _, yaml := range c.removed {
		err = multierr.Append(err, util.KubeApplyContents(c.Namespace, yaml, c.kubeconfig))
	}
	c.applied = []string{}
	return err
}

func (c *deployableConfig) propagationDelay() time.Duration {
	// With multiple clusters, it takes more time to propagate.
	return defaultPropagationDelay * time.Duration(len(tc.Kube.Clusters))
}

type testConfig struct {
	*framework.CommonConfig
	AppDir      string
	extraConfig map[string]*deployableConfig
}

// Setup initializes the test environment and waits for all pods to be in the running state.
func (t *testConfig) Setup() (err error) {
	// Wait for all the pods to be in the running state before starting tests.
	for cluster, kc := range t.Kube.Clusters {
		log.Infof("Making sure all pods are running on cluster: %s", cluster)
		if !util.CheckPodsRunning(t.Kube.Namespace, kc) {
			return fmt.Errorf("can't get all pods running in %s cluster", cluster)
		}
		log.Info("All pods are running")
	}

	// Update the meshNetworks within the mesh config with the gateway address of the remote
	// cluster.
	remoteGwAddr, ingressErr := util.GetIngress(istioIngressGatewayServiceName, istioIngressGatewayLabel,
		t.Kube.Namespace, t.Kube.RemoteKubeConfig, util.LoadBalancerServiceType, false)
	if ingressErr != nil {
		return ingressErr
	}
	util.ReplaceInConfigMap(tc.Kube.Namespace, istioMeshConfigMapName,
		fmt.Sprintf("s/0.0.0.0/%s/", remoteGwAddr), tc.Kube.KubeConfig)

	// Add the remote cluster into the mesh by adding the secret
	if err = addRemoteCluster(); err != nil {
		return
	}

	// Wait until the deployments on remote cluster are all ready
	err = util.CheckDeployments(tc.Kube.Namespace, maxDeploymentTimeout, tc.Kube.RemoteKubeConfig)
	if err != nil {
		return
	}

	// Apply the cluster-aware gateway for the auto SNI-based passthrough. Applied to primary but
	// effective for all clusters.
	err = util.KubeApplyContents(tc.Kube.Namespace, clusterAwareGateway, tc.Kube.KubeConfig)

	// Clusters are now ready for testing

	// Deploy additional configuration/apps
	for _, ec := range t.extraConfig {
		err = ec.Setup()
		if err != nil {
			return
		}
	}

	return
}

// Teardown shuts down the test environment.
func (t *testConfig) Teardown() (err error) {
	// Delete the remote cluster secret
	if err = deleteRemoteCluster(); err != nil {
		return err
	}

	// Remove additional configuration.
	for _, ec := range t.extraConfig {
		e := ec.Teardown()
		if e != nil {
			err = multierr.Append(err, e)
		}
	}
	return
}

func addRemoteCluster() error {
	if err := util.CreateMultiClusterSecret(tc.Kube.Namespace, tc.Kube.RemoteKubeConfig, tc.Kube.KubeConfig); err != nil {
		log.Errorf("Unable to create remote cluster secret on primary cluster %s", err.Error())
		return err
	}
	time.Sleep(defaultPropagationDelay)
	return nil
}

func deleteRemoteCluster() error {
	if err := util.DeleteMultiClusterSecret(tc.Kube.Namespace, tc.Kube.RemoteKubeConfig, tc.Kube.KubeConfig); err != nil {
		return err
	}
	time.Sleep(defaultPropagationDelay)
	return nil
}
