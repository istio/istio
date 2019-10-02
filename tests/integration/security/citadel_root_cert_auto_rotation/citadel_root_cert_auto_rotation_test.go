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

package citadelrootcertautorotation

import (
	"bytes"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/security/pkg/pki/util"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

			// Wait for the next round of root cert upgrade
			time.Sleep(1 * time.Minute)
			newCaScrt, err := kubeAccessor.GetSecret(systemNS.Name()).Get(CASecret, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("unable to load root secret: %s", err.Error())
			}
			verifyRootUpgrade(t, caScrt, newCaScrt)
		})
}

func verifyRootUpgrade(t *testing.T, lastScrt, newScrt *v1.Secret) {
	if lastScrt == nil || newScrt == nil {
		t.Errorf("root secret is nil")
	}
	if bytes.Equal(lastScrt.Data[caCertID], newScrt.Data[caCertID]) {
		t.Errorf("new root cert and old root cert should be different")
	}
	cert, _ := util.ParsePemEncodedCertificate(newScrt.Data[caCertID])
	timeToExpire := cert.NotAfter
	t.Logf("verified that root cert is upgraded successfully, "+
		"ca cert expiration time %v", timeToExpire.String())
}
