//go:build integ
// +build integ

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

package ambient

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/cert"
)

func TestCertificateRefresh(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.cacert-rotation").
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)

			sa := apps.Captured[0].ServiceAccountName()

			// we do not know which ztunnel instance is located on the node as the workload, so we need to check all of them initially
			ztunnelPods, err := kubetest.NewPodFetch(t.AllClusters()[0], istioCfg.SystemNamespace, "app=ztunnel")()
			assert.NoError(t, err)

			originalPem, ztunnelPod, err := getWorkloadIntermediateCert(t, ztunnelPods, sa, istioCtl)
			if err != nil {
				t.Errorf("failed to get intial workload cert: %v", err)
			}

			// Update CA with new intermediate cert
			if err := cert.CreateCustomCASecret(t,
				"ca-cert-alt.pem", "ca-key-alt.pem",
				"cert-chain-alt.pem", "root-cert-combined.pem"); err != nil {
				t.Errorf("failed to update CA secret: %v", err)
			}

			_ = waitForWorkloadCertUpdate(t, ztunnelPod, sa, istioCtl, originalPem)

		})
}

func getWorkloadIntermediateCert(t framework.TestContext, zPods []v1.Pod, serviceAccount string, ctl istioctl.Instance) (string, v1.Pod, error) {
	for _, ztunnel := range zPods {
		podName := fmt.Sprintf("%s.%s", ztunnel.Name, ztunnel.Namespace)
		out, errOut, err := ctl.Invoke([]string{"pc", "s", podName, "-o", "json"})
		if err != nil || errOut != "" {
			t.Errorf("failed to retrieve pod secrets from %s, err: %v errOut: %s", podName, err, errOut)
		}

		dump := []configdump.CertsDump{}
		if err := json.Unmarshal([]byte(out), &dump); err != nil {
			t.Errorf("failed to unmarshal secret dump: %v", err)
		}

		for _, s := range dump {
			if strings.Contains(s.Identity, serviceAccount) {
				if len(s.CertChain) == 0 {
					t.Errorf("cert chain missing in %v for identity: %v", ztunnel.Name, s.Identity)
				}
				return s.CertChain[0].Pem, ztunnel, nil
			}
		}
	}
	return "", v1.Pod{}, errors.New("failed to find workload cert")
}

// Abstracted function to wait for workload cert to be updated
func waitForWorkloadCertUpdate(t framework.TestContext, ztunnelPod v1.Pod, serviceAccount string, istioCtl istioctl.Instance, originalPem string) string {
	newPem := ""
	retry.UntilOrFail(t, func() bool {
		updatedPem, _, err := getWorkloadIntermediateCert(t, []v1.Pod{ztunnelPod}, serviceAccount, istioCtl)
		if err != nil {
			t.Logf("failed to get current workload cert: %v", err)
			return false
		}

		// retry when workload cert is not updated
		if originalPem != updatedPem {
			newPem = updatedPem
			t.Logf("workload cert is updated")
			return true
		}

		return false
	}, retry.Timeout(5*time.Minute), retry.Delay(10*time.Second))
	return newPem
}
