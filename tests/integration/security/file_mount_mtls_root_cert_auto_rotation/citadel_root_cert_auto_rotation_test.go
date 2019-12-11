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

package filemountmtlsrootcertautorotation

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	CASecret = "istio-ca-secret"
	caCertID = "ca-cert.pem"
)

func TestCitadelRootCertUpgrade(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			// Get initial root cert.
			kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
			systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			caScrt, err := kubeAccessor.GetSecret(systemNS.Name()).Get(CASecret, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("unable to load root secret: %s", err.Error())
			}

			// Root cert rotates every 20~40 seconds. Wait until the next round of root
			// cert rotation is completed and verify the root cert.
			err = waitUntilRootCertRotate(t, caScrt, kubeAccessor, systemNS.Name(), 40*time.Second)
			if err != nil {
				t.Errorf("Root cert is not rotated: %s", err.Error())
			}
		})
}

func verifyRootRotation(t *testing.T, lastScrt, newScrt *v1.Secret) error {
	if lastScrt == nil || newScrt == nil {
		return fmt.Errorf("root secret is nil")
	}
	if bytes.Equal(lastScrt.Data[caCertID], newScrt.Data[caCertID]) {
		return fmt.Errorf("new root cert and old root cert are the same")
	}
	cert, _ := util.ParsePemEncodedCertificate(newScrt.Data[caCertID])
	timeToExpire := cert.NotAfter
	t.Logf("verified that root cert is upgraded successfully, "+
		"ca cert expiration time %v", timeToExpire.String())
	return nil
}

// waitUntilRootCertRotate checks root cert in kubernetes secret istio-ca-secret
// and return when the secret is rotated or timeout.
func waitUntilRootCertRotate(t *testing.T, oldSecret *v1.Secret, kubeAccessor *testKube.Accessor,
	namespace string, timeout time.Duration) error {
	start := time.Now()
	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("root cert does not change after %s", timeout.String())
		}
		curScrt, err := kubeAccessor.GetSecret(namespace).Get(CASecret, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get timestamp from secret istio-ca-secret: %s", err.Error())
		}
		if err := verifyRootRotation(t, oldSecret, curScrt); err != nil {
			t.Logf("istio-ca-secret is not changed: %s", err.Error())
		} else {
			t.Logf("istio-ca-secret is changed")
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}
