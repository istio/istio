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

package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	istiokube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const deletionTimeout = 5 * time.Minute

func TestReconcileDelete(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.operator.uninstall_revision").
		Run(func(t framework.TestContext) {
			// For positive casse, use minimal profile, iop file will be deleted
			t.NewSubTest("delete-iop-success").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
				workDir, err := t.CreateTmpDirectory("operator-controller-test")
				if err != nil {
					t.Fatal("failed to create test directory")
				}
				cs := t.Clusters().Default()
				cleanupInClusterCRs(t, cs)
				t.Cleanup(func() {
					cleanupIstioResources(t, cs, istioCtl)
				})
				s := t.Settings()
				initCmd := []string{
					"operator", "init",
					"--hub=" + s.Image.Hub,
					"--tag=" + s.Image.Tag,
					"--manifests=" + ManifestPath,
				}
				// install istio with default config for the first time by running operator init command
				istioCtl.InvokeOrFail(t, initCmd)
				t.TrackResource(&operatorDumper{rev: ""})

				if _, err := cs.Kube().CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: IstioNamespace,
					},
				}, metav1.CreateOptions{}); err != nil {
					_, err := cs.Kube().CoreV1().Namespaces().Get(context.TODO(), IstioNamespace, metav1.GetOptions{})
					if err == nil {
						log.Info("istio namespace already exist")
					} else {
						t.Errorf("failed to create istio namespace: %v", err)
					}
				}
				iopCRFile = filepath.Join(workDir, "iop_cr.yaml")
				r := "v2"
				// later just run `kubectl apply -f newcr.yaml` to apply new installation cr files and verify.
				applyIop(t, t, cs, "minimal", r)

				if err := checkInstallStatus(cs, r); err != nil {
					t.Errorf("failed to check install status: %v", err)
				}

				iopName := revName("test-istiocontrolplane", r)

				log.Infof("delete iop %s", iopName)
				if err := deleteIop(cs, iopName); err != nil {
					t.Errorf("failed to delete iopfile: %v", err)
				}

				retry.UntilSuccessOrFail(t, func() error {
					exist, checkErr := checkIopExist(cs, iopName)
					if checkErr != nil {
						return checkErr
					}

					if exist {
						return fmt.Errorf("fail to delete iop")
					}

					return nil
				}, retry.Timeout(deletionTimeout), retry.Delay(retryDelay))
			})

			// For negative casse, use default profile, ingressgateway alway connect to istiod
			// deletion will fail
			t.NewSubTest("delete-iop-fail").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
				workDir, err := t.CreateTmpDirectory("operator-controller-test")
				if err != nil {
					t.Fatal("failed to create test directory")
				}
				cs := t.Clusters().Default()
				cleanupInClusterCRs(t, cs)
				t.Cleanup(func() {
					cleanupIstioResources(t, cs, istioCtl)
				})
				s := t.Settings()
				initCmd := []string{
					"operator", "init",
					"--hub=" + s.Image.Hub,
					"--tag=" + s.Image.Tag,
					"--manifests=" + ManifestPath,
				}
				// install istio with default config for the first time by running operator init command
				istioCtl.InvokeOrFail(t, initCmd)
				t.TrackResource(&operatorDumper{rev: ""})

				if _, err := cs.Kube().CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: IstioNamespace,
					},
				}, metav1.CreateOptions{}); err != nil {
					_, err := cs.Kube().CoreV1().Namespaces().Get(context.TODO(), IstioNamespace, metav1.GetOptions{})
					if err == nil {
						log.Info("istio namespace already exist")
					} else {
						t.Errorf("failed to create istio namespace: %v", err)
					}
				}
				iopCRFile = filepath.Join(workDir, "iop_cr.yaml")
				r := "v2"
				// later just run `kubectl apply -f newcr.yaml` to apply new installation cr files and verify.
				applyIop(t, t, cs, "default", r)

				if err := checkInstallStatus(cs, r); err != nil {
					t.Errorf("failed to check install status: %v", err)
				}

				iopName := revName("test-istiocontrolplane", r)

				log.Infof("delete iop %s", iopName)
				if err := deleteIop(cs, iopName); err != nil {
					t.Errorf("failed to delete iopfile: %v", err)
				}

				if err := retry.UntilSuccess(func() error {
					exist, checkErr := checkIopExist(cs, iopName)
					if checkErr != nil {
						return checkErr
					}

					if exist {
						return fmt.Errorf("fail to delete iop")
					}

					return nil
				}, retry.Timeout(deletionTimeout), retry.Delay(retryDelay)); err == nil {
					t.Fatal("except iop still exists, but got nil")
				}
			})
		})
}

func applyIop(t framework.TestContext, ctx resource.Context, cs cluster.Cluster, profileName string, revision string) {
	scopes.Framework.Infof(fmt.Sprintf("=== install istio with profile: %s===\n", profileName))
	metadataYAML := `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: %s
  namespace: istio-system
spec:
`
	if revision != "" {
		metadataYAML += "  revision: " + revision + "\n"
	}

	metadataYAML += `
  profile: %s
  installPackagePath: %s
  hub: %s
  tag: %s
  values:
    global:
      imagePullPolicy: %s
`
	s := ctx.Settings()
	overlayYAML := fmt.Sprintf(metadataYAML, revName("test-istiocontrolplane", revision), profileName, ManifestPathContainer,
		s.Image.Hub, s.Image.Tag, s.Image.PullPolicy)

	scopes.Framework.Infof("=== installing with IOP: ===\n%s\n", overlayYAML)

	if err := os.WriteFile(iopCRFile, []byte(overlayYAML), os.ModePerm); err != nil {
		t.Fatalf("failed to write iop cr file: %v", err)
	}

	if err := cs.ApplyYAMLFiles(IstioNamespace, iopCRFile); err != nil {
		t.Fatalf("failed to apply IstioOperator CR file: %s, %v", iopCRFile, err)
	}
}

func checkIopExist(cs istiokube.CLIClient, iopName string) (bool, error) {
	scopes.Framework.Infof("checking IstioOperator CR status")
	gvr := iopv1alpha1.IstioOperatorGVR

	_, err := cs.Dynamic().Resource(gvr).Namespace(IstioNamespace).Get(context.TODO(), iopName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return false, nil
	} else if err == nil {
		return true, nil
	}

	return false, err
}

func deleteIop(cs istiokube.CLIClient, iopName string) error {
	scopes.Framework.Infof("checking IstioOperator CR status")
	gvr := iopv1alpha1.IstioOperatorGVR

	return cs.Dynamic().Resource(gvr).Namespace(IstioNamespace).Delete(context.TODO(), iopName, metav1.DeleteOptions{})
}
