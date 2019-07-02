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

package pilot

import (
	"testing"
	"time"

	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

func TestJobComplete(t *testing.T) {
	jobName := "test-job"

	for cluster := range tc.Kube.Clusters {
		if err := tc.Kube.CheckJobSucceeded(cluster, jobName); err != nil {
			t.Errorf("Job %s not completed successfully", jobName)
		}
	}
}

func TestRestartEnvoy(t *testing.T) {
	podName := "a"

	kubeconfig := tc.Kube.Clusters[framework.PrimaryCluster]
	ns := tc.Kube.Namespace

	_, err := util.Shell("kubectl -n %s exec %s -c istio-proxy --kubeconfig=%s -- pkill envoy", ns, podName, kubeconfig)
	if err != nil {
		t.Errorf("failed kill envoy: %v", err)
	}

	// restart has a backoff retry
	time.Sleep(1 * time.Second)

	_, err = util.Shell("kubectl -n %s exec %s -c istio-proxy --kubeconfig=%s -- pidof envoy", ns, podName, kubeconfig)
	if err != nil {
		t.Errorf("envoy process not found: %v", err)
	}
}
