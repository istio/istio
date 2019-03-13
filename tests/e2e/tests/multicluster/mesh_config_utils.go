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
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const propagationTime = 5 * time.Second

var (
	appsWithSidecar []string
)

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

	_, err := retry.Retry(nil, retryFn)

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
