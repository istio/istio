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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogojsonpb "github.com/gogo/protobuf/jsonpb"
	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util/sanitycheck"
	"istio.io/pkg/log"
)

const (
	IstioNamespace    = "istio-system"
	OperatorNamespace = "istio-operator"
	retryDelay        = time.Second
	retryTimeOut      = 20 * time.Minute
	nsDeletionTimeout = 5 * time.Minute
)

var (
	// ManifestPath is path of local manifests which istioctl operator init refers to.
	ManifestPath = filepath.Join(env.IstioSrc, "manifests")
	// ManifestPathContainer is path of manifests in the operator container for controller to work with.
	ManifestPathContainer = "/var/lib/istio/manifests"
	iopCRFile             = ""
)

func TestController(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
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

			if _, err := cs.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
				ObjectMeta: kubeApiMeta.ObjectMeta{
					Name: IstioNamespace,
				},
			}, kubeApiMeta.CreateOptions{}); err != nil {
				_, err := cs.CoreV1().Namespaces().Get(context.TODO(), IstioNamespace, kubeApiMeta.GetOptions{})
				if err == nil {
					log.Info("istio namespace already exist")
				} else {
					t.Errorf("failed to create istio namespace: %v", err)
				}
			}
			iopCRFile = filepath.Join(workDir, "iop_cr.yaml")
			// later just run `kubectl apply -f newcr.yaml` to apply new installation cr files and verify.
			installWithCRFile(t, t, cs, istioCtl, "demo", "")

			initCmd = []string{
				"operator", "init",
				"--hub=" + s.Image.Hub,
				"--tag=" + s.Image.Tag,
				"--manifests=" + ManifestPath,
				"--revision=" + "v2",
			}
			// install second operator deployment with different revision
			istioCtl.InvokeOrFail(t, initCmd)
			t.TrackResource(&operatorDumper{rev: "v2"})
			installWithCRFile(t, t, cs, istioCtl, "default", "v2")
			installWithCRFile(t, t, cs, istioCtl, "default", "")

			// istio control plane resources expected to be deleted after deleting CRs
			cleanupInClusterCRs(t, cs)

			// test operator remove command
			scopes.Framework.Infof("checking operator remove command")
			removeCmd := []string{
				"operator", "remove",
			}
			istioCtl.InvokeOrFail(t, removeCmd)

			retry.UntilSuccessOrFail(t, func() error {
				for _, n := range []string{"istio-operator", "istio-operator-v2"} {
					if svc, _ := cs.CoreV1().Services(OperatorNamespace).Get(context.TODO(), n, kubeApiMeta.GetOptions{}); svc.Name != "" {
						return fmt.Errorf("got operator service: %s from cluster, expected to be removed", svc.Name)
					}
					if dp, _ := cs.AppsV1().Deployments(OperatorNamespace).Get(context.TODO(), n, kubeApiMeta.GetOptions{}); dp.Name != "" {
						return fmt.Errorf("got operator deployment %s from cluster, expected to be removed", dp.Name)
					}
				}
				return nil
			}, retry.Timeout(retryTimeOut), retry.Delay(retryDelay))
		})
}

func cleanupIstioResources(t framework.TestContext, cs cluster.Cluster, istioCtl istioctl.Instance) {
	scopes.Framework.Infof("cleaning up resources")
	// clean up Istio control plane
	unInstallCmd := []string{
		"x", "uninstall", "--purge", "--skip-confirmation",
	}
	out, _ := istioCtl.InvokeOrFail(t, unInstallCmd)
	t.Logf("uninstall command output: %s", out)
	// clean up operator namespace
	if err := cs.CoreV1().Namespaces().Delete(context.TODO(), OperatorNamespace,
		kube2.DeleteOptionsForeground()); err != nil {
		t.Logf("failed to delete operator namespace: %v", err)
	}
	if err := kube2.WaitForNamespaceDeletion(cs, OperatorNamespace, retry.Timeout(nsDeletionTimeout)); err != nil {
		t.Logf("failed waiting for operator namespace to be deleted: %v", err)
	}
	var err error
	// clean up dynamically created secret and configmaps
	if e := cs.CoreV1().Secrets(IstioNamespace).DeleteCollection(
		context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
		err = multierror.Append(err, e)
	}
	if e := cs.CoreV1().ConfigMaps(IstioNamespace).DeleteCollection(
		context.Background(), kubeApiMeta.DeleteOptions{}, kubeApiMeta.ListOptions{}); e != nil {
		err = multierror.Append(err, e)
	}
	if err != nil {
		scopes.Framework.Errorf("failed to cleanup dynamically created resources: %v", err)
	}
}

// checkInstallStatus check the status of IstioOperator CR from the cluster
func checkInstallStatus(cs istioKube.ExtendedClient, revision string) error {
	scopes.Framework.Infof("checking IstioOperator CR status")
	gvr := schema.GroupVersionResource{
		Group:    "install.istio.io",
		Version:  "v1alpha1",
		Resource: "istiooperators",
	}

	var unhealthyCN []string
	retryFunc := func() error {
		us, err := cs.Dynamic().Resource(gvr).Namespace(IstioNamespace).Get(context.TODO(), revName("test-istiocontrolplane", revision), kubeApiMeta.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get istioOperator resource: %v", err)
		}
		usIOPStatus := us.UnstructuredContent()["status"]
		if usIOPStatus == nil {
			if _, err := cs.CoreV1().Services(OperatorNamespace).Get(context.TODO(), revName("istio-operator", revision),
				kubeApiMeta.GetOptions{}); err != nil {
				return fmt.Errorf("istio operator svc is not ready: %v", err)
			}
			if _, err := kube2.CheckPodsAreReady(kube2.NewPodFetch(cs, OperatorNamespace, "")); err != nil {
				return fmt.Errorf("istio operator pod is not ready: %v", err)
			}

			return fmt.Errorf("status not found from the istioOperator resource")
		}
		usIOPStatus = usIOPStatus.(map[string]interface{})
		iopStatusString, err := json.Marshal(usIOPStatus)
		if err != nil {
			return fmt.Errorf("failed to marshal istioOperator status: %v", err)
		}
		status := &api.InstallStatus{}
		jspb := gogojsonpb.Unmarshaler{AllowUnknownFields: true}
		if err := jspb.Unmarshal(bytes.NewReader(iopStatusString), status); err != nil {
			return fmt.Errorf("failed to unmarshal istioOperator status: %v", err)
		}
		errs := util.Errors{}
		unhealthyCN = []string{}
		if status.Status != api.InstallStatus_HEALTHY {
			errs = util.AppendErr(errs, fmt.Errorf("got IstioOperator status: %v", status.Status))
		}

		for cn, cnstatus := range status.ComponentStatus {
			if cnstatus.Status != api.InstallStatus_HEALTHY {
				unhealthyCN = append(unhealthyCN, cn)
				errs = util.AppendErr(errs, fmt.Errorf("got component: %s status: %v", cn, cnstatus.Status))
			}
		}
		return errs.ToError()
	}
	scopes.Framework.Infof("waiting for IOP to become healthy")
	err := retry.UntilSuccess(retryFunc, retry.Timeout(retryTimeOut), retry.Delay(retryDelay))
	if err != nil {
		return fmt.Errorf("istioOperator status is not healthy: %v", err)
	}
	return nil
}

func cleanupInClusterCRs(t framework.TestContext, cs cluster.Cluster) {
	// clean up hanging installed-state CR from previous tests, failing for errors is not needed here.
	scopes.Framework.Info("cleaning up in-cluster CRs")
	gvr := schema.GroupVersionResource{
		Group:    "install.istio.io",
		Version:  "v1alpha1",
		Resource: "istiooperators",
	}
	crList, err := cs.Dynamic().Resource(gvr).Namespace(IstioNamespace).List(context.TODO(),
		kubeApiMeta.ListOptions{})
	if err == nil {
		for _, obj := range crList.Items {
			t.Logf("deleting CR %v", obj.GetName())
			if err := cs.Dynamic().Resource(gvr).Namespace(IstioNamespace).Delete(context.TODO(), obj.GetName(),
				kubeApiMeta.DeleteOptions{}); err != nil {
				t.Logf("failed to delete existing CR: %v", err)
			}
		}
	} else {
		t.Logf("failed to list existing CR: %v", err.Error())
	}

	scopes.Framework.Infof("waiting for pods in istio-system to be deleted")
	// wait for pods in istio-system to be deleted
	err = retry.UntilSuccess(func() error {
		podList, err := cs.Kube().CoreV1().Pods(IstioNamespace).List(context.TODO(), kubeApiMeta.ListOptions{})
		if err != nil {
			return err
		}
		if len(podList.Items) == 0 {
			return nil
		}
		names := []string{}
		for _, i := range podList.Items {
			names = append(names, i.Name)
		}
		return fmt.Errorf("pods still remain in %s: %v", IstioNamespace, names)
	}, retry.Timeout(retryTimeOut), retry.Delay(retryDelay))

	if err != nil {
		t.Logf("failed to delete pods in %s: %v", IstioNamespace, err)
	} else {
		t.Logf("all pods in istio-system deleted")
	}
}

func installWithCRFile(t framework.TestContext, ctx resource.Context, cs cluster.Cluster,
	istioCtl istioctl.Instance, profileName string, revision string) {
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

	verifyInstallation(t, ctx, istioCtl, profileName, revision, cs)
}

// verifyInstallation verify IOP CR status and compare in-cluster resources with generated ones.
// It also returns the expected K8sObjects generated by manifest generate command.
func verifyInstallation(t framework.TestContext, ctx resource.Context,
	istioCtl istioctl.Instance, profileName string, revision string, cs cluster.Cluster) object.K8sObjects {
	scopes.Framework.Infof("=== verifying istio installation revision %s === ", revision)
	if err := checkInstallStatus(cs, revision); err != nil {
		t.Fatalf("IstioOperator status not healthy: %v", err)
	}

	if _, err := kube2.CheckPodsAreReady(kube2.NewSinglePodFetch(cs, IstioNamespace, "app=istiod")); err != nil {
		t.Fatalf("istiod pod is not ready: %v", err)
	}

	// get manifests by running `manifest generate`
	generateCmd := []string{
		"manifest", "generate",
		"--manifests", ManifestPath,
	}
	if profileName != "" {
		generateCmd = append(generateCmd, "--set", fmt.Sprintf("profile=%s", profileName))
	}
	if revision != "" {
		generateCmd = append(generateCmd, "--revision", revision)
	}
	genManifests, _ := istioCtl.InvokeOrFail(t, generateCmd)
	K8SObjects, err := object.ParseK8sObjectsFromYAMLManifest(genManifests)
	if err != nil {
		t.Errorf("failed to parse generated manifest: %v", err)
	}

	compareInClusterAndGeneratedResources(t, cs, K8SObjects, false)
	sanitycheck.RunTrafficTest(t, ctx)
	scopes.Framework.Infof("=== succeeded ===")
	return K8SObjects
}

func compareInClusterAndGeneratedResources(t framework.TestContext, cs cluster.Cluster, k8sObjects object.K8sObjects,
	expectRemoved bool) {
	// nolint:staticcheck
	if k8sObjects == nil {
		t.Fatalf("expected K8sObjects is nil")
	}

	efgvr := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "envoyfilters",
	}

	// nolint:staticcheck
	for _, genK8SObject := range k8sObjects {
		kind := genK8SObject.Kind
		ns := genK8SObject.Namespace
		name := genK8SObject.Name
		scopes.Framework.Infof("checking kind: %s, namespace: %s, name: %s", kind, ns, name)
		retry.UntilSuccessOrFail(t, func() error {
			var err error
			switch kind {
			case "Service":
				_, err = cs.CoreV1().Services(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
			case "ServiceAccount":
				_, err = cs.CoreV1().ServiceAccounts(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
			case "Deployment":
				_, err = cs.AppsV1().Deployments(IstioNamespace).Get(context.TODO(), name,
					kubeApiMeta.GetOptions{})
			case "ConfigMap":
				_, err = cs.CoreV1().ConfigMaps(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
			case "ValidatingWebhookConfiguration":
				_, err = cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(),
					name, kubeApiMeta.GetOptions{})
			case "MutatingWebhookConfiguration":
				_, err = cs.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(),
					name, kubeApiMeta.GetOptions{})
			case "CustomResourceDefinition":
				_, err = cs.Ext().ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), name,
					kubeApiMeta.GetOptions{})
			case "EnvoyFilter":
				_, err = cs.Dynamic().Resource(efgvr).Namespace(ns).Get(context.TODO(), name,
					kubeApiMeta.GetOptions{})
			case "PodDisruptionBudget":
				_, err = cs.PolicyV1beta1().PodDisruptionBudgets(ns).Get(context.TODO(), name,
					kubeApiMeta.GetOptions{})
			case "HorizontalPodAutoscaler":
				_, err = cs.AutoscalingV2beta1().HorizontalPodAutoscalers(ns).Get(context.TODO(), name,
					kubeApiMeta.GetOptions{})
			}
			if err != nil && !expectRemoved {
				return fmt.Errorf("failed to get expected %s: %s from cluster", kind, name)
			}
			if err == nil && expectRemoved && kind != "CustomResourceDefinition" {
				return fmt.Errorf("%s: %s expected to be removed from cluster but still exists", kind, name)
			}
			return nil
		}, retry.Timeout(time.Second*300), retry.Delay(time.Millisecond*100))
	}
}

func revName(name, revision string) string {
	if revision == "" {
		return name
	}
	return name + "-" + revision
}
