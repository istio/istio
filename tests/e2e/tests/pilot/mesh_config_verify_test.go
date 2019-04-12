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
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const maxDeploymentTimeout = 480 * time.Second
const propagationTime = 5 * time.Second

func verifyMCMeshConfig(primaryPodNames, remotePodNames []string, appEPs map[string][]string) error {
	retry := util.Retrier{
		BaseDelay: 2 * time.Second,
		MaxDelay:  10 * time.Second,
		Retries:   60,
	}

	retryFn := func(_ context.Context, i int) error {
		if err := verifyPods(tc.Kube.Istioctl, primaryPodNames, appEPs); err != nil {
			return err
		}

		if err := verifyPods(tc.Kube.RemoteIstioctl, remotePodNames, appEPs); err != nil {
			return err
		}
		return nil
	}

	_, err := retry.Retry(context.TODO(), retryFn)

	return err
}

func addRemoteCluster() error {
	if err := util.CreateMultiClusterSecret(tc.Kube.Namespace, tc.Kube.RemoteKubeConfig, tc.Kube.KubeConfig); err != nil {
		log.Errorf("Unable to create remote cluster secret on local cluster %s", err.Error())
		return err
	}
	time.Sleep(propagationTime)
	return nil
}

func deleteRemoteCluster() error {
	if err := util.DeleteMultiClusterSecret(tc.Kube.Namespace, tc.Kube.RemoteKubeConfig, tc.Kube.KubeConfig); err != nil {
		return err
	}
	time.Sleep(propagationTime)
	return nil
}

func deployApp(cluster string, deploymentName, serviceName string, port1, port2, port3, port4, port5, port6 int,
	version string, injectProxy bool, headless bool, serviceAccount bool, createService bool) (*framework.App, error) {
	tmpApp := getApp(deploymentName, serviceName, 1, port1, port2, port3, port4, port5, port6, version, injectProxy, headless, serviceAccount, createService)

	var appMgr *framework.AppManager
	if cluster == primaryCluster {
		appMgr = tc.Kube.AppManager
	} else {
		appMgr = tc.Kube.RemoteAppManager
	}

	if err := appMgr.DeployApp(&tmpApp); err != nil {
		return nil, err
	}

	// Wait until the app is ready
	err := util.CheckAppDeployment(tc.Kube.Namespace, deploymentName, maxDeploymentTimeout, tc.Kube.Clusters[cluster])
	return &tmpApp, err
}

func undeployApp(cluster string, deploymentName string, app *framework.App) error {
	var appMgr *framework.AppManager
	if cluster == primaryCluster {
		appMgr = tc.Kube.AppManager
	} else {
		appMgr = tc.Kube.RemoteAppManager
	}

	// Undeploy c-v3
	if err := appMgr.UndeployApp(app); err != nil {
		return err
	}

	// Wait until the app is removed
	err := util.CheckDeploymentRemoved(tc.Kube.Namespace, deploymentName, tc.Kube.Clusters[cluster])
	return err
}

func createAndVerifyMCMeshConfig() error {
	// Collect the pod names and app's endpoints
	primaryPodNames, primaryAppEPs, err := util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.Clusters[primaryCluster], "app")
	if err != nil {
		return err
	}

	remotePodNames, remoteAppEPs, err := util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.Clusters[remoteCluster], "app")
	if err != nil {
		return err
	}

	// Verify that the mesh contains endpoints from both the primary and the remote clusters
	log.Infof("Verify that the mesh contains endpoints from both the primary and the remote clusters")
	aggregatedAppEPs := aggregateAppEPs(primaryAppEPs, remoteAppEPs)

	if err = verifyMCMeshConfig(primaryPodNames, remotePodNames, aggregatedAppEPs); err != nil {
		return err
	}

	// Delete the remote cluster secret
	if err = deleteRemoteCluster(); err != nil {
		return err
	}

	// Verify that the mesh contains endpoints from the primary cluster only
	log.Infof("After deleting remote cluster secret, verify that the mesh only contains endpoints from the primary cluster only")
	if err = verifyMCMeshConfig(primaryPodNames, remotePodNames, primaryAppEPs); err != nil {
		return err
	}

	// Add back the remote cluster by creating a secret and configmap in the primary cluster
	if err = addRemoteCluster(); err != nil {
		return err
	}

	// Add a v3 deployment to app "c" in the remote cluster and verify the mesh configuration
	// Service 'c' has already been created earlier. Don't create it anymore.
	tmpApp, err := deployApp(remoteCluster, "c-v3", "c", 80, 8080, 90, 9090, 70, 7070, "v3", true, false, true, false)
	if err != nil {
		return err
	}

	log.Infof("After deploying c-v3, verify that c-v3 is added in the mesh")
	// Get updates on the remote cluster
	remotePodNames, remoteAppEPs, err = util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.Clusters[remoteCluster], "app")
	if err == nil {
		aggregatedAppEPs = aggregateAppEPs(primaryAppEPs, remoteAppEPs)
		// Verify that the mesh contains endpoints from both the primary and the remote clusters
		err = verifyMCMeshConfig(primaryPodNames, remotePodNames, aggregatedAppEPs)
	}

	if err != nil {
		tc.Kube.RemoteAppManager.UndeployApp(tmpApp)
		return err
	}

	// Remove the v3 deployment to app "c" in the remote cluster and verify the mesh configuration
	log.Infof("After deleting c-v3, verify that c-v3 is deleted from the mesh")
	if err = undeployApp(remoteCluster, "c-v3", tmpApp); err != nil {
		return err
	}

	// Verify that proxy config changes back to before adding c-v3
	remotePodNames, remoteAppEPs, err = util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.Clusters[remoteCluster], "app")
	if err == nil {
		aggregatedAppEPs = aggregateAppEPs(primaryAppEPs, remoteAppEPs)
		if err = verifyMCMeshConfig(primaryPodNames, remotePodNames, aggregatedAppEPs); err != nil {
			return err
		}
	}

	return nil
}

func verifyMeshConfig() error {
	// Collect the pod names and app's endpoints
	podNames, appEPs, err := util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.KubeConfig, "app")
	if err != nil {
		return err
	}

	if err = verifyPods(tc.Kube.Istioctl, podNames, appEPs); err != nil {
		return err
	}

	// Add a v3 deployment to app "c" and verify the mesh configuration
	// Service 'c' has already been created earlier. Don't create it anymore.
	tmpApp, err := deployApp(primaryCluster, "c-v3", "c", 80, 8080, 90, 9090, 70, 7070, "v3", true, false, true, false)
	if err != nil {
		return err
	}

	if podNames, appEPs, err = util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.KubeConfig, "app"); err == nil {
		err = verifyPods(tc.Kube.Istioctl, podNames, appEPs)
	}

	if err != nil {
		tc.Kube.AppManager.UndeployApp(tmpApp)
		return err
	}

	// Remove the v3 deployment to app "c" in the remote cluster and verify the mesh configuration
	log.Infof("After deleting c-v3, verify that c-v3 is deleted from the mesh")
	if err = undeployApp(primaryCluster, "c-v3", tmpApp); err != nil {
		return err
	}

	if podNames, appEPs, err = util.GetAppPodsInfo(tc.Kube.Namespace, tc.Kube.KubeConfig, "app"); err != nil {
		return err
	}

	// Verify that proxy config changes back to before adding c-v3
	if err = verifyPods(tc.Kube.Istioctl, podNames, appEPs); err != nil {
		return err
	}
	return nil
}

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
	if !func() bool {
		for _, app := range appsWithSidecar {
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
				err = fmt.Errorf("endpoints for app '%s' in proxy config in pod %s are not correct, got: %v but expected: %v", app, podName, IPs, appEPs[app])
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
