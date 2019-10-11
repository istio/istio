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

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/util"

	"istio.io/operator/pkg/apis/istio/v1alpha2"

	"istio.io/operator/pkg/object"

	// For kubeclient GCP auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	kubectlutil "k8s.io/kubectl/pkg/util/deployment"

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
			name.CNIComponentName,
			name.PrometheusOperatorComponentName,
			name.PrometheusComponentName,
			name.GrafanaComponentName,
			name.KialiComponentName,
			name.TracingComponentName,
		},
	}

	installTree      = make(componentTree)
	dependencyWaitCh = make(map[name.ComponentName]chan struct{})
	kubectl          = kubectlcmd.New()

	k8sRESTConfig  *rest.Config
	appliedObjects = object.K8sObjects{}
)

func init() {
	buildInstallTree()
	for _, parent := range componentDependencies {
		for _, child := range parent {
			dependencyWaitCh[child] = make(chan struct{}, 1)
		}
	}

}

// ParseK8SYAMLToIstioControlPlaneSpec parses a IstioControlPlane CustomResource YAML string and unmarshals in into
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

// InstallOptions contains the startup options for applying the manifest.
type InstallOptions struct {
	// DryRun performs all steps except actually applying the manifests or creating output dirs/files.
	DryRun bool
	// Verbose enables verbose debug output.
	Verbose bool
	// Wait for resources to be ready after install.
	Wait bool
	// Maximum amount of time to wait for resources to be ready after install when Wait=true.
	WaitTimeout time.Duration
	// Path to the kubeconfig file.
	Kubeconfig string
	// Name of the kubeconfig context to use.
	Context string
}

// ApplyAll applies all given manifests using kubectl client.
func ApplyAll(manifests name.ManifestMap, version version.Version, opts *InstallOptions) (CompositeOutput, error) {
	logAndPrint("Applying manifests for these components:")
	for c := range manifests {
		logAndPrint("- %s", c)
	}
	logAndPrint("Component dependencies tree: \n%s", installTreeString())
	if err := initK8SRestClient(opts.Kubeconfig, opts.Context); err != nil {
		return nil, err
	}
	return applyRecursive(manifests, version, opts)
}

func applyRecursive(manifests name.ManifestMap, version version.Version, opts *InstallOptions) (CompositeOutput, error) {
	var wg sync.WaitGroup
	out := CompositeOutput{}
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
			out[c] = applyManifest(c, m, version, opts)

			// Signal all the components that depend on us.
			for _, ch := range componentDependencies[c] {
				logAndPrint("unblocking child dependency %s.", ch)
				dependencyWaitCh[ch] <- struct{}{}
			}
			wg.Done()
		}()
	}
	wg.Wait()
	if opts.Wait {
		return out, waitForResources(appliedObjects, opts)
	}
	return out, nil
}

func applyManifest(componentName name.ComponentName, manifestStr string, version version.Version, opts *InstallOptions) *ComponentApplyOutput {
	objects, err := object.ParseK8sObjectsFromYAMLManifest(manifestStr)
	if err != nil {
		return &ComponentApplyOutput{
			Err: err,
		}
	}
	if len(objects) == 0 {
		return &ComponentApplyOutput{}
	}

	namespace, stdoutCRD, stderrCRD := "", "", ""
	for _, o := range objects {
		o.AddLabels(map[string]string{istioComponentLabelStr: string(componentName)})
		o.AddLabels(map[string]string{operatorLabelStr: operatorReconcileStr})
		o.AddLabels(map[string]string{istioVersionLabelStr: version.String()})
		if o.Namespace != "" {
			// All objects in a component have the same namespace.
			namespace = o.Namespace
		}
	}
	objects.Sort(defaultObjectOrder())

	appliedObjects = append(appliedObjects, objects...)

	// TODO; add "--prune" back (istio/istio#17236)
	extraArgs := []string{"--force", "--selector", fmt.Sprintf("%s=%s", operatorLabelStr, operatorReconcileStr)}

	logAndPrint("kubectl applying manifest for component %s", componentName)

	crdObjects := cRDKindObjects(objects)
	if len(crdObjects) > 0 {
		mcrd, err := crdObjects.JSONManifest()
		if err != nil {
			return &ComponentApplyOutput{
				Err: err,
			}
		}

		stdoutCRD, stderrCRD, err = kubectl.Apply(opts.DryRun, opts.Verbose, opts.Kubeconfig, opts.Context, namespace, mcrd, extraArgs...)
		if err != nil {
			// Not all Istio components are robust to not yet created CRDs.
			if err := waitForCRDs(objects, opts.DryRun); err != nil {
				return &ComponentApplyOutput{
					Stdout: stdoutCRD,
					Stderr: stderrCRD,
					Err:    err,
				}
			}
		}
	}

	stdout, stderr := "", ""
	m, err := objects.JSONManifest()
	if err != nil {
		return &ComponentApplyOutput{
			Stdout: stdoutCRD,
			Stderr: stderrCRD,
			Err:    err,
		}
	}
	stdout, stderr, err = kubectl.Apply(opts.DryRun, opts.Verbose, opts.Kubeconfig, opts.Context, namespace, m, extraArgs...)
	logAndPrint("finished applying manifest for component %s", componentName)
	ym, _ := objects.YAMLManifest()
	return &ComponentApplyOutput{
		Stdout:   stdoutCRD + "\n" + stdout,
		Stderr:   stderrCRD + "\n" + stderr,
		Manifest: ym,
		Err:      err,
	}
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

// waitForResources polls to get the current status of all pods, PVCs, and Services
// until all are ready or a timeout is reached
// TODO - plumb through k8s client and remove global `k8sRESTConfig`
func waitForResources(objects object.K8sObjects, opts *InstallOptions) error {
	if opts.DryRun {
		logAndPrint("Not waiting for resources ready in dry run mode.")
		return nil
	}

	logAndPrint("Waiting for resources ready with timeout of %v", opts.WaitTimeout)
	cs, err := kubernetes.NewForConfig(k8sRESTConfig)
	if err != nil {
		return fmt.Errorf("k8s client error: %s", err)
	}

	errPoll := wait.Poll(2*time.Second, opts.WaitTimeout, func() (bool, error) {
		pods := []v1.Pod{}
		services := []v1.Service{}
		deployments := []deployment{}

		for _, o := range objects {
			kind := o.GroupVersionKind().Kind
			switch kind {
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
			case "Service":
				svc, err := cs.CoreV1().Services(o.Namespace).Get(o.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				services = append(services, *svc)
			}
		}
		isReady := podsReady(pods) && deploymentsReady(deployments) && servicesReady(services)
		return isReady, nil
	})

	if errPoll != nil {
		logAndPrint("Failed to wait for resources ready: %v", errPoll)
		return fmt.Errorf("failed to wait for resources ready: %s", errPoll)
	}

	logAndPrint("Resources are ready.")
	return nil
}

func getPods(client kubernetes.Interface, namespace string, selector map[string]string) ([]v1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	return list.Items, err
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
			logAndPrint("Deployment is not ready: %s/%s", v.deployment.GetNamespace(), v.deployment.GetName())
			return false
		}
	}
	return true
}

func servicesReady(svc []v1.Service) bool {
	for _, s := range svc {
		if s.Spec.Type == v1.ServiceTypeExternalName {
			continue
		}
		if s.Spec.ClusterIP != v1.ClusterIPNone && s.Spec.ClusterIP == "" {
			logAndPrint("Service is not ready: %s/%s", s.GetNamespace(), s.GetName())
			return false
		}
		if s.Spec.Type == v1.ServiceTypeLoadBalancer && s.Status.LoadBalancer.Ingress == nil {
			logAndPrint("Service is not ready: %s/%s", s.GetNamespace(), s.GetName())
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

func initK8SRestClient(kubeconfig, context string) error {
	var err error
	if k8sRESTConfig != nil {
		return nil
	}
	k8sRESTConfig, err = defaultRestConfig(kubeconfig, context)
	if err != nil {
		return err
	}
	return nil
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

func logAndPrint(v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	log.Infof(s)
	fmt.Println(s)
}
