// Copyright 2018 Istio Authors
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

package pilot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

func verifyEndpoints(actualEps []string, proxyEps []string) bool {
	sort.Strings(actualEps)
	sort.Strings(proxyEps)
	if len(actualEps) != len(proxyEps) {
		return false
	}
	for i, ip := range actualEps {
		if ip != proxyEps[i] {
			return false
		}
	}
	return true
}

func verifyPod(istioctl *framework.Istioctl, podName string, appEPs map[string][]string) error {
	// Only verify app Pods that have a sidecar
	apps := []string{"a-", "b-", "c-", "d-", "headless-"}
	if !func() bool {
		for _, app := range apps {
			if strings.HasPrefix(podName, app) {
				return true
			}
		}
		return false
	}() {
		return nil
	}

	// Get proxy endpoint configuration
	epInfo, err := istioctl.GetProxyConfigEndpoints(podName, []string{"a", "b", "c", "d", "headless"})
	if err != nil {
		return err
	}
	for app, portIPs := range epInfo {
		for _, IPs := range portIPs {
			if !verifyEndpoints(appEPs[app], IPs) {
				err = fmt.Errorf("Endpoints for app '%s' in proxy config in pod %s are not correct: %v vs %v", app, podName, IPs, appEPs[app])
				log.Errorf("%v", err)
				return err
			}
		}
	}
	log.Infof("Pod %s mesh configuration verified", podName)
	return nil
}

func verifyPods(istioctl *framework.Istioctl, podNames []string, appEPs map[string][]string) (err error) {
	for _, podName := range podNames {
		if err = verifyPod(istioctl, podName, appEPs); err != nil {
			return
		}
	}
	return
}

func aggregateAppEPs(firstAppEPs, secondAppEPs map[string][]string) map[string][]string {
	aaeps := make(map[string][]string)
	for app, eps := range firstAppEPs {
		aaeps[app] = append(aaeps[app], eps...)
	}
	for app, eps := range secondAppEPs {
		aaeps[app] = append(aaeps[app], eps...)
	}
	return aaeps
}

func (t *testConfig) addAndVerifyRemoteCluster() error {
	// Collect the pod names and app's endpoints
	primaryPodNames, primaryAppEPs, err := util.GetAppPodsInfo(t.Kube.Namespace, t.Kube.Clusters[primaryCluster], "app")
	if err != nil {
		return err
	}

	remotePodNames, remoteAppEPs, err := util.GetAppPodsInfo(t.Kube.Namespace, t.Kube.Clusters[remoteCluster], "app")
	if err != nil {
		return err
	}

	// Verify that the mesh contains endpoints from the primary cluster only
	log.Infof("Before adding remote cluster secret, verify that the emesh only contains endpoints from the primary cluster only")
	if err = verifyPods(t.Kube.Istioctl, primaryPodNames, primaryAppEPs); err != nil {
		return err
	}

	if err = verifyPods(t.Kube.RemoteIstioctl, remotePodNames, primaryAppEPs); err != nil {
		return err
	}

	// Add the remote cluster by creating a secret and configmap in the primary cluster
	if err = util.CreateMultiClusterSecret(t.Kube.Namespace, t.Kube.RemoteKubeConfig, t.Kube.KubeConfig); err != nil {
		log.Errorf("Unable to create remote cluster secret on local cluster %s", err.Error())
		return err
	}

	// Wait a few seconds for the mesh to be reconfigured
	time.Sleep(5 * time.Second)

	// Verify that the mesh contains endpoints from both the primary and the remote clusters
	log.Infof("After adding remote cluster secret, verify that the emesh contains endpoints from both the primary and the remote clusters")
	aggregatedAppEPs := aggregateAppEPs(primaryAppEPs, remoteAppEPs)

	if err = verifyPods(t.Kube.Istioctl, primaryPodNames, aggregatedAppEPs); err != nil {
		return err
	}

	if err = verifyPods(t.Kube.RemoteIstioctl, remotePodNames, aggregatedAppEPs); err != nil {
		return err
	}

	// Delete the remote cluster secret
	if err = util.DeleteMultiClusterSecret(t.Kube.Namespace, t.Kube.RemoteKubeConfig, t.Kube.KubeConfig); err != nil {
		return err
	}

	// Wait a few seconds for the mesh to be reconfigured
	time.Sleep(5 * time.Second)

	log.Infof("After deleting remote cluster secret, verify again that the emesh contains endpoints from the primary cluster only")
	// Verify that the mesh contains the primary endpoints only
	if err = verifyPods(t.Kube.Istioctl, primaryPodNames, primaryAppEPs); err != nil {
		return err
	}

	if err = verifyPods(t.Kube.RemoteIstioctl, remotePodNames, primaryAppEPs); err != nil {
		return err
	}

	// Again, add the remote cluster by creating a secret and configmap in the primary cluster
	if err = util.CreateMultiClusterSecret(t.Kube.Namespace, t.Kube.RemoteKubeConfig, t.Kube.KubeConfig); err != nil {
		log.Errorf("Unable to create remote cluster secret on local cluster %s", err.Error())
		return err
	}

	// Wait a few seconds for the mesh to be reconfigured
	time.Sleep(5 * time.Second)

	// Add and remove a v3 deployment to app "c" in the remote cluster and verify the mesh configuration
	// Service 'c' has already been created earlier. Don't create it anymore.
	tmpApp := getApp("c-v3", "c", 80, 8080, 90, 9090, 70, 7070, "v3", true, false, true, false)

	if err = t.Kube.RemoteAppManager.DeployApp(&tmpApp); err != nil {
		return err
	}

	// Wait a few seconds for the mesh to be reconfigured
	time.Sleep(30 * time.Second)

	// Get updates on the remote cluster
	remotePodNames1, remoteAppEPs1, err := util.GetAppPodsInfo(t.Kube.Namespace, t.Kube.Clusters[remoteCluster], "app")
	if err != nil {
		t.Kube.RemoteAppManager.UndeployApp(&tmpApp)
		return err
	}

	log.Infof("After deploying c-v3, verify that c-v3 is added in the mesh")
	// Verify that the mesh contains endpoints from both the primary and the remote clusters
	aggregatedAppEPs1 := aggregateAppEPs(primaryAppEPs, remoteAppEPs1)

	if err = verifyPods(t.Kube.Istioctl, primaryPodNames, aggregatedAppEPs1); err != nil {
		t.Kube.RemoteAppManager.UndeployApp(&tmpApp)
		return err
	}

	if err = verifyPods(t.Kube.RemoteIstioctl, remotePodNames1, aggregatedAppEPs1); err != nil {
		t.Kube.RemoteAppManager.UndeployApp(&tmpApp)
		return err
	}

	// Undeploy c-v3
	t.Kube.RemoteAppManager.UndeployApp(&tmpApp)

	// Wait a few seconds for the mesh to be reconfigured
	time.Sleep(30 * time.Second)

	log.Infof("After deleting c-v3, verify that c-v3 is deleted from the mesh")
	// Verify that proxy config changes back to before adding c-v3
	if err = verifyPods(t.Kube.Istioctl, primaryPodNames, aggregatedAppEPs); err != nil {
		return err
	}

	if err = verifyPods(t.Kube.RemoteIstioctl, remotePodNames, aggregatedAppEPs); err != nil {
		return err
	}

	return nil
}
