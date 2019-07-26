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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"istio.io/operator/pkg/util"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/apis/istio/v1alpha2"

	"istio.io/operator/pkg/object"

	// For kubeclient GCP auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/operator/pkg/kubectlcmd"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/version"
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
)

// CompositeOutput is used to capture errors and stdout/stderr outputs for a command, per component.
type CompositeOutput struct {
	// Stdout is the stdout output.
	Stdout map[name.ComponentName]string
	// Stderr is the stderr output.
	Stderr map[name.ComponentName]string
	// Error is the error output.
	Err map[name.ComponentName]error
}

// NewCompositeOutput creates a new CompositeOutput and returns a ptr to it.
func NewCompositeOutput() *CompositeOutput {
	return &CompositeOutput{
		Stdout: make(map[name.ComponentName]string),
		Stderr: make(map[name.ComponentName]string),
		Err:    make(map[name.ComponentName]error),
	}
}

type componentNameToListMap map[name.ComponentName][]name.ComponentName
type componentTree map[name.ComponentName]interface{}

var (
	componentDependencies = componentNameToListMap{
		name.IstioBaseComponentName: {
			name.PilotComponentName,
			name.PolicyComponentName,
			name.TelemetryComponentName,
			name.GalleyComponentName,
			name.CitadelComponentName,
			name.NodeAgentComponentName,
			name.CertManagerComponentName,
			name.SidecarInjectorComponentName,
			name.IngressComponentName,
			name.EgressComponentName,
		},
	}

	installTree      = make(componentTree)
	dependencyWaitCh = make(map[name.ComponentName]chan struct{})
	kubectl          = kubectlcmd.New()

	k8sRESTConfig *rest.Config
)

func init() {
	buildInstallTree()
	for _, parent := range componentDependencies {
		for _, child := range parent {
			dependencyWaitCh[child] = make(chan struct{}, 1)
		}
	}

}

// ParseK8SYAMLToIstioControlPlaneSpec parses a YAML string IstioControlPlane CustomResource and unmarshals in into
// an IstioControlPlaneSpec object. It returns the object and an API group/version with it.
func ParseK8SYAMLToIstioControlPlaneSpec(yml string) (*v1alpha2.IstioControlPlaneSpec, *schema.GroupVersionKind, error) {
	o, err := object.ParseYAMLToK8sObject([]byte(yml))
	if err != nil {
		return nil, nil, err
	}
	y, err := yaml.Marshal(o.UnstructuredObject().Object["spec"])
	if err != nil {
		return nil, nil, err
	}
	icp := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(string(y), icp); err != nil {
		return nil, nil, err
	}
	gvk := o.GroupVersionKind()
	return icp, &gvk, nil
}

// RenderToDir writes manifests to a local filesystem directory tree.
func RenderToDir(manifests name.ManifestMap, outputDir string, dryRun, verbose bool) error {
	logAndPrint("Component dependencies tree: \n%s", installTreeString())
	logAndPrint("Rendering manifests to output dir %s", outputDir)
	return renderRecursive(manifests, installTree, outputDir, dryRun, verbose)
}

func renderRecursive(manifests name.ManifestMap, installTree componentTree, outputDir string, dryRun, verbose bool) error {
	for k, v := range installTree {
		componentName := string(k)
		ym := manifests[k]
		if ym == "" {
			logAndPrint("Manifest for %s not found, skip.", componentName)
			continue
		}
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
		if err := renderRecursive(manifests, kt, dirName, dryRun, verbose); err != nil {
			return err
		}
	}
	return nil
}

// ApplyAll applies all given manifests using kubectl client.
func ApplyAll(manifests name.ManifestMap, version version.Version, dryRun, verbose bool) (*CompositeOutput, error) {
	logAndPrint("Applying manifests for these components:")
	for c := range manifests {
		logAndPrint("- %s", c)
	}
	logAndPrint("Component dependencies tree: \n%s", installTreeString())
	if err := initK8SRestClient(); err != nil {
		return nil, err
	}
	return applyRecursive(manifests, version, dryRun, verbose), nil
}

func applyRecursive(manifests name.ManifestMap, version version.Version, dryRun, verbose bool) *CompositeOutput {
	var wg sync.WaitGroup
	out := NewCompositeOutput()
	for c, m := range manifests {
		c := c
		m := m
		wg.Add(1)
		go func() {
			if s := dependencyWaitCh[c]; s != nil {
				logAndPrint("%s is waiting on parent dependency...", c)
				<-s
				logAndPrint("Parent dependency for %s has unblocked, proceeding.", c)
			}
			out.Stdout[c], out.Stderr[c], out.Err[c] = applyManifest(c, m, version, dryRun, verbose)

			// Signal all the components that depend on us.
			for _, ch := range componentDependencies[c] {
				logAndPrint("unblocking child dependency %s.", ch)
				dependencyWaitCh[ch] <- struct{}{}
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return out
}

func versionString(version version.Version) string {
	return version.String()
}

func applyManifest(componentName name.ComponentName, manifestStr string, version version.Version, dryRun, verbose bool) (string, string, error) {
	objects, err := object.ParseK8sObjectsFromYAMLManifest(manifestStr)
	if err != nil {
		return "", "", err
	}
	if len(objects) == 0 {
		return "", "", nil
	}

	namespace, stdoutCRD, stderrCRD := "", "", ""
	for _, o := range objects {
		o.AddLabels(map[string]string{istioComponentLabelStr: string(componentName)})
		o.AddLabels(map[string]string{operatorLabelStr: operatorReconcileStr})
		o.AddLabels(map[string]string{istioVersionLabelStr: versionString(version)})
		if o.Namespace != "" {
			// All objects in a component have the same namespace.
			namespace = o.Namespace
		}
	}
	objects.Sort(defaultObjectOrder())

	// TODO; add "--prune" back
	extraArgs := []string{"--force", "--selector", fmt.Sprintf("%s=%s", operatorLabelStr, operatorReconcileStr)}

	logAndPrint("kubectl applying manifest for component %s", componentName)

	crdObjects := cRDKindObjects(objects)
	if len(crdObjects) > 0 {
		mcrd, err := crdObjects.JSONManifest()
		if err != nil {
			return "", "", err
		}

		stdoutCRD, stderrCRD, err = kubectl.Apply(dryRun, verbose, namespace, mcrd, extraArgs...)
		if err != nil {
			// Not all Istio components are robust to not yet created CRDs.
			if err := waitForCRDs(objects, dryRun); err != nil {
				return stdoutCRD, stderrCRD, err
			}
		}
	}

	log.Infof("Applying the following manifest:\n%s", manifestStr)
	stdout, stderr := "", ""
	m, err := objects.JSONManifest()
	if err != nil {
		return stdoutCRD, stderrCRD, err
	}
	stdout, stderr, err = kubectl.Apply(dryRun, verbose, namespace, m, extraArgs...)
	logAndPrint("finished applying manifest for component %s", componentName)
	return stdoutCRD + "\n" + stdout, stderrCRD + "\n" + stderr, err
}

func defaultObjectOrder() func(o *object.K8sObject) int {
	return func(o *object.K8sObject) int {
		gk := o.Group + "/" + o.Kind
		switch gk {
		// Create CRDs asap - both because they are slow and because we will likely create instances of them soon
		case "apiextensions.k8s.io/CustomResourceDefinition":
			return -1000

			// We need to create ServiceAccounts, Roles before we bind them with a RoleBinding
		case "/ServiceAccount", "rbac.authorization.k8s.io/ClusterRole":
			return 1
		case "rbac.authorization.k8s.io/ClusterRoleBinding":
			return 2

			// Pods might need configmap or secrets - avoid backoff by creating them first
		case "/ConfigMap", "/Secrets":
			return 100

			// Create the pods after we've created other things they might be waiting for
		case "extensions/Deployment", "app/Deployment":
			return 1000

			// Autoscalers typically act on a deployment
		case "autoscaling/HorizontalPodAutoscaler":
			return 1001

			// Create services late - after pods have been started
		case "/Service":
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

func waitForCRDs(objects object.K8sObjects, dryRun bool) error {
	if dryRun {
		logAndPrint("Not waiting for CRDs in dry run mode.")
		return nil
	}

	logAndPrint("Waiting for CRDs to be applied.")
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
						log.Infof("established CRD %q", crdName)
						continue descriptor
					}
				case apiextensionsv1beta1.NamesAccepted:
					if cond.Status == apiextensionsv1beta1.ConditionFalse {
						log.Warnf("name conflict: %v", cond.Reason)
					}
				}
			}
			log.Infof("missing status condition for %q", crdName)
			return false, nil
		}
		return true, nil
	})

	if errPoll != nil {
		logAndPrint("failed to verify CRD creation; %s", errPoll)
		return fmt.Errorf("failed to verify CRD creation: %s", errPoll)
	}

	logAndPrint("CRDs applied.")
	return nil
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

func initK8SRestClient() error {
	var err error
	if k8sRESTConfig != nil {
		return nil
	}
	k8sRESTConfig, err = defaultRestConfig("", "")
	if err != nil {
		return err
	}
	/*	k8sRESTConfig, err = rest.RESTClientFor(config)
		if err != nil {
			return nil, err
		}
		return &Client{config, restClient}, nil
	*/
	return nil
}

func defaultRestConfig(kubeconfig, configContext string) (*rest.Config, error) {
	config, err := BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
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

func logAndPrint(v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	log.Infof(s)
	fmt.Println(s)
}
