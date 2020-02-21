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

package manifest

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time" // For kubeclient GCP auth

	"github.com/ghodss/yaml"
	goversion "github.com/hashicorp/go-version"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	// For GCP auth functionality.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	kubectlutil "k8s.io/kubectl/pkg/util/deployment"
	"k8s.io/utils/pointer"

	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/kubectlcmd"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	pkgversion "istio.io/istio/operator/pkg/version"
	binversion "istio.io/istio/operator/version"
	"istio.io/pkg/log"
)

const (
	// cRDPollInterval is how often the state of CRDs is polled when waiting for their creation.
	cRDPollInterval = 500 * time.Millisecond
	// cRDPollTimeout is the maximum wait time for all CRDs to be created.
	cRDPollTimeout = 60 * time.Second

	// operatorReconcileStr indicates that the operator will reconcile the resource.
	operatorReconcileStr = "Reconcile"
)

var (
	// operatorLabelStr indicates Istio operator is managing this resource.
	operatorLabelStr = name.OperatorAPINamespace + "/managed"
	// istioComponentLabelStr indicates which Istio component a resource belongs to.
	istioComponentLabelStr = name.OperatorAPINamespace + "/component"
	// istioVersionLabelStr indicates the Istio version of the installation.
	istioVersionLabelStr = name.OperatorAPINamespace + "/version"

	scope = log.RegisterScope("installer", "installer", 0)
)

// ComponentApplyOutput is used to capture errors and stdout/stderr outputs for a command, per component.
type ComponentApplyOutput struct {
	// Stdout is the stdout output.
	Stdout string
	// Stderr is the stderr output.
	Stderr string
	// Error is the error output.
	Err error
	// Manifest is the manifest applied to the cluster.
	Manifest string
}

type CompositeOutput map[name.ComponentName]*ComponentApplyOutput

type componentNameToListMap map[name.ComponentName][]name.ComponentName
type componentTree map[name.ComponentName]interface{}

// deployment holds associated replicaSets for a deployment
type deployment struct {
	replicaSets *appsv1.ReplicaSet
	deployment  *appsv1.Deployment
}

var (
	componentDependencies = componentNameToListMap{
		name.PilotComponentName: {
			name.PolicyComponentName,
			name.TelemetryComponentName,
			name.GalleyComponentName,
			name.CitadelComponentName,
			name.CNIComponentName,
			name.IngressComponentName,
			name.EgressComponentName,
			name.AddonComponentName,
		},
		name.IstioBaseComponentName: {
			name.PilotComponentName,
		},
	}

	installTree      = make(componentTree)
	dependencyWaitCh = make(map[name.ComponentName]chan struct{})
	kubectl          = kubectlcmd.New()

	k8sRESTConfig     *rest.Config
	currentKubeconfig string
	currentContext    string
	// TODO: remove whitelist after : https://github.com/kubernetes/kubernetes/issues/66430
	defaultPilotPruneWhileList = []string{
		// kubectl apply prune default
		"core/v1/Pod",
		"core/v1/ConfigMap",
		"core/v1/Service",
		"core/v1/Secret",
		"core/v1/Endpoints",
		"core/v1/Namespace",
		"core/v1/PersistentVolume",
		"core/v1/PersistentVolumeClaim",
		"core/v1/ReplicationController",
		"batch/v1/Job",
		"batch/v1beta1/CronJob",
		"extensions/v1beta1/Ingress",
		"apps/v1/DaemonSet",
		"apps/v1/Deployment",
		"apps/v1/ReplicaSet",
		"apps/v1/StatefulSet",
		"networking.istio.io/v1alpha3/DestinationRule",
		"networking.istio.io/v1alpha3/EnvoyFilter",
	}
	componentPruneWhiteList = map[name.ComponentName][]string{
		name.PilotComponentName: defaultPilotPruneWhileList,
	}
)

func init() {
	buildInstallTree()
	for _, parent := range componentDependencies {
		for _, child := range parent {
			dependencyWaitCh[child] = make(chan struct{}, 1)
		}
	}

}

// ParseK8SYAMLToIstioOperator parses a IstioOperator CustomResource YAML string and unmarshals in into
// an IstioOperatorSpec object. It returns the object and an API group/version with it.
func ParseK8SYAMLToIstioOperator(yml string) (*iopv1alpha1.IstioOperator, *schema.GroupVersionKind, error) {
	o, err := object.ParseYAMLToK8sObject([]byte(yml))
	if err != nil {
		return nil, nil, err
	}
	iop := &iopv1alpha1.IstioOperator{}
	if err := util.UnmarshalWithJSONPB(yml, iop, false); err != nil {
		return nil, nil, err
	}
	gvk := o.GroupVersionKind()
	iopv1alpha1.SetNamespace(iop.Spec, o.Namespace)
	return iop, &gvk, nil
}

// RenderToDir writes manifests to a local filesystem directory tree.
func RenderToDir(manifests name.ManifestMap, outputDir string, dryRun bool) error {
	logAndPrint("Component dependencies tree: \n%s", installTreeString())
	logAndPrint("Rendering manifests to output dir %s", outputDir)
	return renderRecursive(manifests, installTree, outputDir, dryRun)
}

func renderRecursive(manifests name.ManifestMap, installTree componentTree, outputDir string, dryRun bool) error {
	for k, v := range installTree {
		componentName := string(k)
		// In cases (like gateways) where multiple instances can exist, concatenate the manifests and apply as one.
		ym := strings.Join(manifests[k], helm.YAMLSeparator)
		logAndPrint("Rendering: %s", componentName)
		dirName := filepath.Join(outputDir, componentName)
		if !dryRun {
			if err := os.MkdirAll(dirName, os.ModePerm); err != nil {
				return fmt.Errorf("could not create directory %s; %s", outputDir, err)
			}
		}
		fname := filepath.Join(dirName, componentName) + ".yaml"
		logAndPrint("Writing manifest to %s", fname)
		if !dryRun {
			if err := ioutil.WriteFile(fname, []byte(ym), 0644); err != nil {
				return fmt.Errorf("could not write manifest config; %s", err)
			}
		}

		kt, ok := v.(componentTree)
		if !ok {
			// Leaf
			return nil
		}
		if err := renderRecursive(manifests, kt, dirName, dryRun); err != nil {
			return err
		}
	}
	return nil
}

// Version runs the `kubectl version` and returns kubectl version and k8s server version
func Version(opts *kubectlcmd.Options) (*goversion.Version, *goversion.Version, error) {
	stdout, stderr, kubectlErr := kubectl.Version(opts)
	if kubectlErr != nil && stderr != "" && strings.TrimSpace(stdout) == "" {
		return nil, nil, fmt.Errorf("%s: %s", kubectlErr, stderr)
	}
	return parseKubectlVersion(stdout)
}

func parseKubectlVersion(kubectlStdout string) (*goversion.Version, *goversion.Version, error) {
	yamlObj := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(kubectlStdout), &yamlObj)
	if err != nil {
		return nil, nil, err
	}
	var errs util.Errors
	var clientVersion, serverVersion *goversion.Version
	cPath := util.ToYAMLPath("clientVersion.gitVersion")
	cNode, cFound, err := tpath.GetFromTreePath(yamlObj, cPath)
	errs = util.AppendErr(errs, err)
	if cFound {
		var cVer string
		if cVer, err = pkgversion.TagToVersionString(cNode.(string)); err == nil {
			clientVersion, err = goversion.NewVersion(cVer)
		}
		errs = util.AppendErr(errs, err)
	}
	sPath := util.ToYAMLPath("serverVersion.gitVersion")
	sNode, sFound, err := tpath.GetFromTreePath(yamlObj, sPath)
	errs = util.AppendErr(errs, err)
	if sFound {
		var sVer string
		if sVer, err = pkgversion.TagToVersionString(sNode.(string)); err == nil {
			serverVersion, err = goversion.NewVersion(sVer)
		}
		errs = util.AppendErr(errs, err)
	}
	return clientVersion, serverVersion, errs.ToError()
}

// ApplyAll applies all given manifests using kubectl client.
func ApplyAll(manifests name.ManifestMap, version pkgversion.Version, opts *kubectlcmd.Options) (CompositeOutput, error) {
	scope.Infof("Preparing manifests for these components:")
	for c := range manifests {
		scope.Infof("- %s", c)
	}
	scope.Infof("Component dependencies tree: \n%s", installTreeString())
	if _, err := InitK8SRestClient(opts.Kubeconfig, opts.Context); err != nil {
		return nil, err
	}
	return applyRecursive(manifests, version, opts)
}

func applyRecursive(manifests name.ManifestMap, version pkgversion.Version, opts *kubectlcmd.Options) (CompositeOutput, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := CompositeOutput{}
	allAppliedObjects := object.K8sObjects{}
	for c, m := range manifests {
		c := c
		m := m
		wg.Add(1)
		go func() {
			if s := dependencyWaitCh[c]; s != nil {
				scope.Infof("%s is waiting on a prerequisite...", c)
				<-s
				scope.Infof("Prerequisite for %s has completed, proceeding with install.", c)
			}
			applyOut, appliedObjects := ApplyManifest(c, strings.Join(m, helm.YAMLSeparator), version.String(), *opts)
			mu.Lock()
			out[c] = applyOut
			allAppliedObjects = append(allAppliedObjects, appliedObjects...)
			mu.Unlock()

			// If we are depending on a component, we may depend on it actually running (eg Deployment is ready)
			// For example, for the validation webhook to become ready, so we should wait for it always.
			if len(componentDependencies[c]) > 0 {
				if err := WaitForResources(appliedObjects, opts); err != nil {
					scope.Errorf("failed to wait for resource: %v", err)
				}
			}
			// Signal all the components that depend on us.
			for _, ch := range componentDependencies[c] {
				scope.Infof("unblocking child %s.", ch)
				dependencyWaitCh[ch] <- struct{}{}
			}
			wg.Done()
		}()
	}
	wg.Wait()
	if opts.Wait {
		return out, WaitForResources(allAppliedObjects, opts)
	}
	return out, nil
}

func ApplyManifest(componentName name.ComponentName, manifestStr, version string,
	opts kubectlcmd.Options) (*ComponentApplyOutput, object.K8sObjects) {
	stdout, stderr := "", ""
	appliedObjects := object.K8sObjects{}
	objects, err := object.ParseK8sObjectsFromYAMLManifest(manifestStr)
	if err != nil {
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}
	componentLabel := fmt.Sprintf("%s=%s", istioComponentLabelStr, componentName)

	// TODO: remove this when `kubectl --prune` supports empty objects
	//  (https://github.com/kubernetes/kubernetes/issues/40635)
	// Delete all resources for a disabled component
	if len(objects) == 0 && !opts.DryRun {
		getOpts := opts
		getOpts.Output = "yaml"
		getOpts.ExtraArgs = []string{"--all-namespaces", "--selector", componentLabel}
		stdoutGet, stderrGet, err := kubectl.GetAll(&getOpts)
		if err != nil {
			stdout += "\n" + stdoutGet
			stderr += "\n" + stderrGet
			return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
		}
		items, err := GetKubectlGetItems(stdoutGet)
		if err != nil {
			return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
		}
		if len(items) == 0 {
			return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
		}

		logAndPrint("- Pruning objects for disabled component %s...", componentName)
		delObjects, err := object.ParseK8sObjectsFromYAMLManifest(stdoutGet)
		if err != nil {
			return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
		}
		delOpts := opts
		delOpts.Output = ""
		delOpts.ExtraArgs = []string{"--selector", componentLabel}
		stdoutDel, stderrDel, err := kubectl.Delete(stdoutGet, &delOpts)
		stdout += "\n" + stdoutDel
		stderr += "\n" + stderrDel
		if err != nil {
			logAndPrint("✘ Finished pruning objects for disabled component %s.", componentName)
			return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
		}
		appliedObjects = append(appliedObjects, delObjects...)
		logAndPrint("✔ Finished pruning objects for disabled component %s.", componentName)
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}

	for _, o := range objects {
		o.AddLabels(map[string]string{istioComponentLabelStr: string(componentName)})
		o.AddLabels(map[string]string{operatorLabelStr: operatorReconcileStr})
		o.AddLabels(map[string]string{istioVersionLabelStr: version})
	}

	opts.ExtraArgs = []string{"--force", "--selector", componentLabel}
	// Base components include namespaces and CRDs, pruning them will remove user configs, which makes it hard to roll back.
	if componentName != name.IstioBaseComponentName && opts.Prune == nil {
		opts.Prune = pointer.BoolPtr(true)
		pwl, ok := componentPruneWhiteList[componentName]
		if ok {
			for _, pw := range pwl {
				pwa := []string{"--prune-whitelist", pw}
				opts.ExtraArgs = append(opts.ExtraArgs, pwa...)
			}
		}
	}

	logAndPrint("- Applying manifest for component %s...", componentName)

	// Apply namespace resources first, then wait.
	nsObjects := nsKindObjects(objects)
	stdout, stderr, err = applyObjects(nsObjects, &opts, stdout, stderr)
	if err != nil {
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}
	if err := WaitForResources(nsObjects, &opts); err != nil {
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}
	appliedObjects = append(appliedObjects, nsObjects...)

	// Apply CRDs, then wait.
	crdObjects := cRDKindObjects(objects)
	stdout, stderr, err = applyObjects(crdObjects, &opts, stdout, stderr)
	if err != nil {
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}
	if err := waitForCRDs(crdObjects, stdout, opts.DryRun); err != nil {
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}
	appliedObjects = append(appliedObjects, crdObjects...)

	// Apply all remaining objects.
	// We sort them by namespace so that we can pass the `-n` to the apply command. This is required for prune to work
	// See https://github.com/kubernetes/kubernetes/issues/87756 for details
	namespaces, nonNsCrdObjectsByNamespace := splitByNamespace(objectsNotInLists(objects, nsObjects, crdObjects))
	var applyErr error
	for _, ns := range namespaces {
		nonNsCrdObjects := nonNsCrdObjectsByNamespace[ns]
		nsOpts := opts
		nsOpts.Namespace = ns
		stdout, stderr, err = applyObjects(nonNsCrdObjects, &nsOpts, stdout, stderr)
		if err != nil {
			applyErr = fmt.Errorf("error applying object to %v: %v: %v", ns, err, applyErr)
		}
		appliedObjects = append(appliedObjects, nonNsCrdObjects...)
	}
	mark := "✔"
	if applyErr != nil {
		mark = "✘"
	}
	logAndPrint("%s Finished applying manifest for component %s.", mark, componentName)
	if applyErr != nil {
		return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
	}
	return buildComponentApplyOutput(stdout, stderr, appliedObjects, err), appliedObjects
}

// Split objects by namespace, and return a sorted list of keys defining the order they should be applied
func splitByNamespace(objs object.K8sObjects) ([]string, map[string]object.K8sObjects) {
	res := map[string]object.K8sObjects{}
	order := []string{}
	for _, obj := range objs {
		if _, f := res[obj.Namespace]; !f {
			order = append(order, obj.Namespace)
		}
		res[obj.Namespace] = append(res[obj.Namespace], obj)
	}
	// Sort in alphabetical order. The key thing here is that the clusterwide resources are applied first.
	// Clusterwide resources have no namespace, so they are sorted first.
	sort.Strings(order)
	return order, res
}

func GetKubectlGetItems(stdoutGet string) ([]interface{}, error) {
	yamlGet := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(stdoutGet), &yamlGet)
	if err != nil {
		return nil, err
	}
	if yamlGet["kind"] != "List" {
		return nil, fmt.Errorf("`kubectl get` returned YAML whose kind is not List")
	}
	if _, ok := yamlGet["items"]; !ok {
		return nil, fmt.Errorf("`kubectl get` returned YAML without 'items'")
	}
	switch items := yamlGet["items"].(type) {
	case []interface{}:
		return items, nil
	}
	return nil, fmt.Errorf("`kubectl get` returned incorrect 'items' type")
}

func DeploymentExists(kubeconfig, context, namespace, name string) (bool, error) {
	if _, err := InitK8SRestClient(kubeconfig, context); err != nil {
		return false, err
	}

	cs, err := kubernetes.NewForConfig(k8sRESTConfig)
	if err != nil {
		return false, fmt.Errorf("k8s client error: %s", err)
	}

	d, err := cs.AppsV1().Deployments(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return d != nil, nil
}

func applyObjects(objs object.K8sObjects, opts *kubectlcmd.Options, stdout, stderr string) (string, string, error) {
	if len(objs) == 0 {
		return stdout, stderr, nil
	}

	objs.Sort(DefaultObjectOrder())

	mns, err := objs.JSONManifest()
	if err != nil {
		return stdout, stderr, err
	}

	stdoutApply, stderrApply, err := kubectl.Apply(mns, opts)
	stdout += "\n" + stdoutApply
	stderr += "\n" + stderrApply

	return stdout, stderr, err
}

func buildComponentApplyOutput(stdout string, stderr string, objects object.K8sObjects, err error) *ComponentApplyOutput {
	manifest, _ := objects.YAMLManifest()
	return &ComponentApplyOutput{
		Stdout:   stdout,
		Stderr:   stderr,
		Manifest: manifest,
		Err:      err,
	}
}

func istioCustomResources(group string) bool {
	switch group {
	case "config.istio.io",
		"rbac.istio.io",
		"security.istio.io",
		"authentication.istio.io",
		"networking.istio.io":
		return true
	}
	return false
}

// DefaultObjectOrder is default sorting function used to sort k8s objects.
func DefaultObjectOrder() func(o *object.K8sObject) int {
	return func(o *object.K8sObject) int {
		gk := o.Group + "/" + o.Kind
		switch {
		// Create CRDs asap - both because they are slow and because we will likely create instances of them soon
		case gk == "apiextensions.k8s.io/CustomResourceDefinition":
			return -1000

			// We need to create ServiceAccounts, Roles before we bind them with a RoleBinding
		case gk == "/ServiceAccount" || gk == "rbac.authorization.k8s.io/ClusterRole":
			return 1
		case gk == "rbac.authorization.k8s.io/ClusterRoleBinding":
			return 2

			// validatingwebhookconfiguration is configured to FAIL-OPEN in the default install. For the
			// re-install case we want to apply the validatingwebhookconfiguration first to reset any
			// orphaned validatingwebhookconfiguration that is FAIL-CLOSE.
		case gk == "admissionregistration.k8s.io/ValidatingWebhookConfiguration":
			return 3

		case istioCustomResources(o.Group):
			return 4

			// Pods might need configmap or secrets - avoid backoff by creating them first
		case gk == "/ConfigMap" || gk == "/Secrets":
			return 100

			// Create the pods after we've created other things they might be waiting for
		case gk == "extensions/Deployment" || gk == "app/Deployment":
			return 1000

			// Autoscalers typically act on a deployment
		case gk == "autoscaling/HorizontalPodAutoscaler":
			return 1001

			// Create services late - after pods have been started
		case gk == "/Service":
			return 10000

		default:
			return 1000
		}
	}
}

func cRDKindObjects(objects object.K8sObjects) object.K8sObjects {
	var ret object.K8sObjects
	for _, o := range objects {
		if o.Kind == "CustomResourceDefinition" {
			ret = append(ret, o)
		}
	}
	return ret
}

func nsKindObjects(objects object.K8sObjects) object.K8sObjects {
	var ret object.K8sObjects
	for _, o := range objects {
		if o.Kind == "Namespace" {
			ret = append(ret, o)
		}
	}
	return ret
}

func objectsNotInLists(objects object.K8sObjects, lists ...object.K8sObjects) object.K8sObjects {
	var ret object.K8sObjects

	filterMap := make(map[*object.K8sObject]bool)
	for _, list := range lists {
		for _, object := range list {
			filterMap[object] = true
		}
	}

	for _, o := range objects {
		if !filterMap[o] {
			ret = append(ret, o)
		}
	}
	return ret
}

func canSkipCrdWait(applyOut string) bool {
	for _, line := range strings.Split(applyOut, "\n") {
		if line == "" {
			continue
		}
		segments := strings.Split(line, " ")
		if len(segments) == 2 {
			changed := segments[1] != "unchanged"
			isCrd := strings.HasPrefix(segments[0], "customresourcedefinition")
			if changed && isCrd {
				return false
			}
		}
	}
	return true
}

func waitForCRDs(objects object.K8sObjects, stdout string, dryRun bool) error {
	if dryRun {
		scope.Info("Not waiting for CRDs in dry run mode.")
		return nil
	}

	if canSkipCrdWait(stdout) {
		scope.Info("Skipping CRD wait, no changes detected")
		return nil
	}
	scope.Info("Waiting for CRDs to be applied.")
	cs, err := apiextensionsclient.NewForConfig(k8sRESTConfig)
	if err != nil {
		return fmt.Errorf("k8s client error: %s", err)
	}

	var crdNames []string
	for _, o := range cRDKindObjects(objects) {
		crdNames = append(crdNames, o.Name)
	}

	errPoll := wait.Poll(cRDPollInterval, cRDPollTimeout, func() (bool, error) {
	descriptor:
		for _, crdName := range crdNames {
			crd, errGet := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crdName, metav1.GetOptions{})
			if errGet != nil {
				return false, errGet
			}
			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case apiextensionsv1beta1.Established:
					if cond.Status == apiextensionsv1beta1.ConditionTrue {
						scope.Infof("established CRD %s", crdName)
						continue descriptor
					}
				case apiextensionsv1beta1.NamesAccepted:
					if cond.Status == apiextensionsv1beta1.ConditionFalse {
						scope.Warnf("name conflict for %v: %v", crdName, cond.Reason)
					}
				}
			}
			scope.Infof("missing status condition for %q", crdName)
			return false, nil
		}
		return true, nil
	})

	if errPoll != nil {
		scope.Errorf("failed to verify CRD creation; %s", errPoll)
		return fmt.Errorf("failed to verify CRD creation: %s", errPoll)
	}

	scope.Info("Finished applying CRDs.")
	return nil
}

// WaitForResources polls to get the current status of all pods, PVCs, and Services
// until all are ready or a timeout is reached
// TODO - plumb through k8s client and remove global `k8sRESTConfig`
func WaitForResources(objects object.K8sObjects, opts *kubectlcmd.Options) error {
	if opts.DryRun {
		logAndPrint("Not waiting for resources ready in dry run mode.")
		return nil
	}

	cs, err := kubernetes.NewForConfig(k8sRESTConfig)
	if err != nil {
		return fmt.Errorf("k8s client error: %s", err)
	}

	errPoll := wait.Poll(2*time.Second, opts.WaitTimeout, func() (bool, error) {
		pods := []v1.Pod{}
		deployments := []deployment{}
		namespaces := []v1.Namespace{}

		for _, o := range objects {
			kind := o.GroupVersionKind().Kind
			switch kind {
			case "Namespace":
				namespace, err := cs.CoreV1().Namespaces().Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				namespaces = append(namespaces, *namespace)
			case "Pod":
				pod, err := cs.CoreV1().Pods(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				pods = append(pods, *pod)
			case "ReplicationController":
				rc, err := cs.CoreV1().ReplicationControllers(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				list, err := getPods(cs, rc.Namespace, rc.Spec.Selector)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case "Deployment":
				currentDeployment, err := cs.AppsV1().Deployments(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				_, _, newReplicaSet, err := kubectlutil.GetAllReplicaSets(currentDeployment, cs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case "DaemonSet":
				ds, err := cs.AppsV1().DaemonSets(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				list, err := getPods(cs, ds.Namespace, ds.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case "StatefulSet":
				sts, err := cs.AppsV1().StatefulSets(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				list, err := getPods(cs, sts.Namespace, sts.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case "ReplicaSet":
				rs, err := cs.AppsV1().ReplicaSets(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				list, err := getPods(cs, rs.Namespace, rs.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			}
		}
		isReady := namespacesReady(namespaces) && podsReady(pods) && deploymentsReady(deployments)
		if !isReady {
			logAndPrint("  Waiting for resources to become ready...")
		}
		return isReady, nil
	})

	if errPoll != nil {
		msg := fmt.Sprintf("resources not ready after %v: %v", opts.WaitTimeout, errPoll)
		logAndPrint(msg)
		return errors.New(msg)
	}
	return nil
}

func getPods(client kubernetes.Interface, namespace string, selector map[string]string) ([]v1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	return list.Items, err
}

func namespacesReady(namespaces []v1.Namespace) bool {
	for _, namespace := range namespaces {
		if !isNamespaceReady(&namespace) {
			logAndPrint("Namespace is not ready: %s", namespace.GetName())
			return false
		}
	}
	return true
}

func podsReady(pods []v1.Pod) bool {
	for _, pod := range pods {
		if !isPodReady(&pod) {
			logAndPrint("Pod is not ready: %s/%s", pod.GetNamespace(), pod.GetName())
			return false
		}
	}
	return true
}

func isNamespaceReady(namespace *v1.Namespace) bool {
	return namespace.Status.Phase == v1.NamespaceActive
}

func isPodReady(pod *v1.Pod) bool {
	if len(pod.Status.Conditions) > 0 {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == v1.PodReady &&
				condition.Status == v1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

func deploymentsReady(deployments []deployment) bool {
	for _, v := range deployments {
		if v.replicaSets.Status.ReadyReplicas < *v.deployment.Spec.Replicas {
			scope.Infof("Deployment is not ready: %s/%s", v.deployment.GetNamespace(), v.deployment.GetName())
			return false
		}
	}
	return true
}

func buildInstallTree() {
	// Starting with root, recursively insert each first level child into each node.
	insertChildrenRecursive(name.IstioBaseComponentName, installTree, componentDependencies)
}

func insertChildrenRecursive(componentName name.ComponentName, tree componentTree, children componentNameToListMap) {
	tree[componentName] = make(componentTree)
	for _, child := range children[componentName] {
		insertChildrenRecursive(child, tree[componentName].(componentTree), children)
	}
}

func installTreeString() string {
	var sb strings.Builder
	buildInstallTreeString(name.IstioBaseComponentName, "", &sb)
	return sb.String()
}

func buildInstallTreeString(componentName name.ComponentName, prefix string, sb io.StringWriter) {
	_, _ = sb.WriteString(prefix + string(componentName) + "\n")
	if _, ok := installTree[componentName].(componentTree); !ok {
		return
	}
	for k := range installTree[componentName].(componentTree) {
		buildInstallTreeString(k, prefix+"  ", sb)
	}
}

func InitK8SRestClient(kubeconfig, context string) (*rest.Config, error) {
	var err error
	if kubeconfig == currentKubeconfig && context == currentContext && k8sRESTConfig != nil {
		return k8sRESTConfig, nil
	}
	currentKubeconfig, currentContext = kubeconfig, context

	k8sRESTConfig, err = defaultRestConfig(kubeconfig, context)
	if err != nil {
		return nil, err
	}
	return k8sRESTConfig, nil
}

func defaultRestConfig(kubeconfig, configContext string) (*rest.Config, error) {
	config, err := BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

// BuildClientConfig is a helper function that builds client config from a kubeconfig filepath.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / empty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	//Config loading rules:
	// 1. kubeconfig if it not empty string
	// 2. In cluster config if running in-cluster
	// 3. Config(s) in KUBECONFIG environment variable
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

// CheckK8sVersion checks if the current client and server version is supported
func CheckK8sVersion(opts *kubectlcmd.Options) error {
	cK8sVer, sK8sVer, err := Version(opts)
	if err != nil {
		return fmt.Errorf("failed to read versions from client and cluster: %v", err)
	}
	compatibleMap, err := pkgversion.GetVersionCompatibleMap("", binversion.OperatorBinaryGoVersion)
	if err != nil {
		return err
	}
	return checkK8sVersion(compatibleMap, cK8sVer, sK8sVer)
}

// checkK8sVersion checks if the client and server versions are supported in CompatibilityMapping
func checkK8sVersion(vmap *pkgversion.CompatibilityMapping, cVer, sVer *goversion.Version) error {
	if cVer == nil {
		return fmt.Errorf("failed to get Kubernetes client version, please check if kubectl is correctly installed")
	}
	if sVer == nil {
		return fmt.Errorf("failed to get Kubernetes server version, please check if the k8s context is configured")
	}
	if vmap.K8sClientVersionRange != nil && !vmap.K8sClientVersionRange.Check(cVer) {
		return fmt.Errorf("failed to meet the requirements of kubectl version for Istio %s: "+
			"current kubectl version: %s, supported version range: %s",
			binversion.OperatorCodeBaseVersion, cVer, vmap.K8sClientVersionRange)
	}
	if vmap.K8sServerVersionRange != nil && !vmap.K8sServerVersionRange.Check(sVer) {
		return fmt.Errorf("failed to meet the requirements of k8s server version for Istio %s: "+
			"current k8s server version: %s, supported version range: %s",
			binversion.OperatorCodeBaseVersion, sVer, vmap.K8sServerVersionRange)
	}
	return nil
}

func logAndPrint(v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	scope.Infof(s)
	fmt.Println(s)
}
