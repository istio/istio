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

package extratrustanchor

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/citadel"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/security/pkg/k8s/configmap"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/tests/integration/security/util/secret"
)

func serialNumbersFromCert(cert []byte) []string {
	var sn []string
	for len(cert) > 0 {
		var block *pem.Block
		block, cert = pem.Decode(cert)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		sn = append(sn, cert.SerialNumber.String())
	}
	return sn
}

func strLess(a, b string) bool { return a < b }

type operation int

const (
	opAdd operation = iota
	opRemove
)

func createConfigMapOrFail(t test.Failer, env *kube.Environment, namespace string, cm *kubeApiCore.ConfigMap) *kubeApiCore.ConfigMap {
	t.Helper()
	created, err := env.CreateConfigMap(namespace, cm)
	if err != nil {
		t.Fatalf("Could not create configmap %v in namespace %v: %v", cm.Name, namespace, err)
	}
	return created
}

func deleteConfigMapOrFail(t test.Failer, env *kube.Environment, namespace, name string) {
	t.Helper()
	if err := env.DeleteConfigMap(namespace, name); err != nil {
		t.Fatalf("Could not delete configmap %v in namespace %v: %v", name, namespace, err)
	}
}

const waitDelay = time.Second

func updateAndCheck(t *testing.T, env *kube.Environment, c citadel.Instance, cm *kubeApiCore.ConfigMap, namespace string, op operation, roots []string) {
	if cm != nil {
		if op == opAdd {
			createConfigMapOrFail(t, env, namespace, cm)
		} else {
			deleteConfigMapOrFail(t, env, namespace, cm.GetName())
		}

		// ConfigMap updates are fast. Give Citadel a second to refresh its internal state.
		time.Sleep(waitDelay)

		// Secret updates are slow sometimes (1-2 minutes). Force citadel to regenerate the
		// secret with the new roots after a configmap change.
		c.DeleteSecretOrFail(t, citadel.SecretName)
	}

	updated := c.WaitForSecretToExistOrFail(t)
	secret.ExamineOrFail(t, updated)

	// Verify the updated secret's roots match citadel's root concatenated with any additional trusted roots.
	gotRoots := updated.Data[controller.RootCertID]
	wantRoots := []byte(strings.Join(roots, "\n"))

	gotSN := serialNumbersFromCert(gotRoots)
	wantSN := serialNumbersFromCert(wantRoots)

	if diff := cmp.Diff(gotSN, wantSN, cmpopts.SortSlices(strLess)); diff != "" {
		t.Fatalf("wrong set of roots (by serial number): \n gotRoots %v \nwantRoots %v \ndiff %v", gotSN, wantSN, diff)
	}
}

func makeConfigMap(name string, labels map[string]string, data map[string]string) *kubeApiCore.ConfigMap {
	return &kubeApiCore.ConfigMap{
		ObjectMeta: kubeApiMeta.ObjectMeta{Name: name, Labels: labels},
		Data:       data,
	}
}

func getCitadelRootCA(env *kube.Environment, namespace string) (string, error) {
	cm, err := env.GetConfigMap(namespace, configmap.IstioSecurityConfigMapName)
	if err != nil {
		return "", err
	}
	encoded := cm.Data[configmap.CATLSRootCertName]
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(bytes), "\n"), nil
}

// Test assumes --max-workload-cert-ttl is 90 days.
func TestExtraTrustAnchor(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		c := citadel.NewOrFail(t, ctx, citadel.Config{Istio: ist})
		namespace := ist.Settings().SystemNamespace
		env := ctx.Environment().(*kube.Environment)

		rootCA, err := getCitadelRootCA(env, namespace)
		if err != nil {
			t.Fatalf("failed to get initial root CA: %v", err)
		}

		var (
			labelsTrue  = map[string]string{controller.ExtraTrustAnchorsLabel: "true"}
			labelsFalse = map[string]string{controller.ExtraTrustAnchorsLabel: "false"}
			labelsNone  = map[string]string{}

			root1Data     = map[string]string{"root1": root1}
			root2Data     = map[string]string{"root2": root2}
			root3Data     = map[string]string{"root3": root3}
			root4And5Data = map[string]string{"root4": root4, "root5": root5}

			cmMissingLabelRoot1      = makeConfigMap("root1", labelsNone, root1Data)
			cmLabeledFalseRoot2      = makeConfigMap("root2", labelsFalse, root2Data)
			cmSingleRootRoot3        = makeConfigMap("root3", labelsTrue, root3Data)
			cmMultipleRootsRoot4And5 = makeConfigMap("root4And5", labelsTrue, root4And5Data)
		)

		steps := []struct {
			name      string
			configmap *kubeApiCore.ConfigMap
			op        operation
			roots     []string
		}{
			{
				name:  "default root",
				roots: []string{rootCA},
			},
			{
				name:      "add configmap with missing label",
				op:        opAdd,
				configmap: cmMissingLabelRoot1,
				roots:     []string{rootCA},
			},
			{
				name:      "add config map wrong label",
				op:        opAdd,
				configmap: cmLabeledFalseRoot2,
				roots:     []string{rootCA},
			},
			{
				name:      "add configmap with single root",
				op:        opAdd,
				configmap: cmSingleRootRoot3,
				roots:     []string{rootCA, root3},
			},
			{
				name:      "add configmap with multiple roots",
				op:        opAdd,
				configmap: cmMultipleRootsRoot4And5,
				roots:     []string{rootCA, root3, root4, root5}, // plus root3 from previous step
			},
			{
				name:      "remove configmap with single root",
				op:        opRemove,
				configmap: cmSingleRootRoot3,
				roots:     []string{rootCA, root4, root5},
			},
			{
				name:      "remove configmap with multiple roots",
				op:        opRemove,
				configmap: cmMultipleRootsRoot4And5,
				roots:     []string{rootCA},
			},
		}

		for i, step := range steps {
			t.Run(fmt.Sprintf("[%v] %v", i, step), func(tt *testing.T) {
				updateAndCheck(tt, env, c, step.configmap, namespace, step.op, step.roots)
			})
		}
	})
}
