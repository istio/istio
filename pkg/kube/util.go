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

package kube

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" //  allow out of cluster authentication
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	istioversion "istio.io/istio/pkg/version"
)

var cronJobNameRegexp = regexp.MustCompile(`(.+)-\d{8,10}$`)

// BuildClientConfig builds a client rest config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	c, err := BuildClientCmd(kubeconfig, context).ClientConfig()
	if err != nil {
		return nil, err
	}
	return SetRestDefaults(c), nil
}

// BuildClientCmd builds a client cmd config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientCmd(kubeconfig, context string, overrides ...func(*clientcmd.ConfigOverrides)) clientcmd.ClientConfig {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / empty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	// Config loading rules:
	// 1. kubeconfig if it not empty string
	// 2. Config(s) in KUBECONFIG environment variable
	// 3. In cluster config if running in-cluster
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
	}

	for _, fn := range overrides {
		fn(configOverrides)
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}

// CreateClientset is a helper function that builds a kubernetes Clienset from a kubeconfig
// filepath. See `BuildClientConfig` for kubeconfig loading rules.
func CreateClientset(kubeconfig, context string, fns ...func(*rest.Config)) (*kubernetes.Clientset, error) {
	c, err := BuildClientConfig(kubeconfig, context)
	if err != nil {
		return nil, fmt.Errorf("build client config: %v", err)
	}
	for _, fn := range fns {
		fn(c)
	}
	return kubernetes.NewForConfig(c)
}

// DefaultRestConfig returns the rest.Config for the given kube config file and context.
func DefaultRestConfig(kubeconfig, configContext string, fns ...func(*rest.Config)) (*rest.Config, error) {
	config, err := BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	config = SetRestDefaults(config)

	for _, fn := range fns {
		fn(config)
	}

	return config, nil
}

// adjustCommand returns the last component of the
// OS-specific command path for use in User-Agent.
func adjustCommand(p string) string {
	// Unlikely, but better than returning "".
	if len(p) == 0 {
		return "unknown"
	}
	return filepath.Base(p)
}

// IstioUserAgent returns the user agent string based on the command being used.
// example: pilot-discovery/1.9.5 or istioctl/1.10.0
// This is a specialized version of rest.DefaultKubernetesUserAgent()
func IstioUserAgent() string {
	return adjustCommand(os.Args[0]) + "/" + istioversion.Info.Version
}

// SetRestDefaults is a helper function that sets default values for the given rest.Config.
// This function is idempotent.
func SetRestDefaults(config *rest.Config) *rest.Config {
	if config.GroupVersion == nil || config.GroupVersion.Empty() {
		config.GroupVersion = &corev1.SchemeGroupVersion
	}
	if len(config.APIPath) == 0 {
		if len(config.GroupVersion.Group) == 0 {
			config.APIPath = "/api"
		} else {
			config.APIPath = "/apis"
		}
	}
	if len(config.ContentType) == 0 {
		if features.KubernetesClientContentType == "json" {
			config.ContentType = runtime.ContentTypeJSON
		} else {
			// Prefer to accept protobuf, but send JSON. This is due to some types (CRDs)
			// not accepting protobuf.
			// If we end up writing many core types in the future we may want to set ContentType to
			// ContentTypeProtobuf only for the core client.
			config.AcceptContentTypes = runtime.ContentTypeProtobuf + "," + runtime.ContentTypeJSON
			config.ContentType = runtime.ContentTypeJSON
		}
	}
	if config.NegotiatedSerializer == nil {
		// This codec factory ensures the resources are not converted. Therefore, resources
		// will not be round-tripped through internal versions. Defaulting does not happen
		// on the client.
		config.NegotiatedSerializer = serializer.NewCodecFactory(IstioScheme).WithoutConversion()
	}
	if len(config.UserAgent) == 0 {
		config.UserAgent = IstioUserAgent()
	}

	return config
}

// CheckPodReadyOrComplete returns nil if the given pod and all of its containers are ready or terminated
// successfully.
func CheckPodReadyOrComplete(pod *corev1.Pod) error {
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		return nil
	case corev1.PodRunning:
		return CheckPodReady(pod)
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

// CheckPodReady returns nil if the given pod and all of its containers are ready.
func CheckPodReady(pod *corev1.Pod) error {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// Wait until all containers are ready.
		for _, containerStatus := range pod.Status.InitContainerStatuses {
			if !containerStatus.Ready {
				return fmt.Errorf("init container not ready: '%s'", containerStatus.Name)
			}
		}
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return fmt.Errorf("container not ready: '%s'", containerStatus.Name)
			}
		}
		if len(pod.Status.Conditions) > 0 {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
					return fmt.Errorf("pod not ready, condition message: %v", condition.Message)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

// GetDeployMetaFromPod heuristically derives deployment metadata from the pod spec.
func GetDeployMetaFromPod(pod *corev1.Pod) (metav1.ObjectMeta, metav1.TypeMeta) {
	if pod == nil {
		return metav1.ObjectMeta{}, metav1.TypeMeta{}
	}
	// try to capture more useful namespace/name info for deployments, etc.
	// TODO(dougreid): expand to enable lookup of OWNERs recursively a la kubernetesenv
	deployMeta := pod.ObjectMeta
	deployMeta.ManagedFields = nil
	deployMeta.OwnerReferences = nil

	typeMetadata := metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	}
	if len(pod.GenerateName) > 0 {
		// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.
		var controllerRef metav1.OwnerReference
		controllerFound := false
		for _, ref := range pod.GetOwnerReferences() {
			if ref.Controller != nil && *ref.Controller {
				controllerRef = ref
				controllerFound = true
				break
			}
		}
		if controllerFound {
			typeMetadata.APIVersion = controllerRef.APIVersion
			typeMetadata.Kind = controllerRef.Kind

			// heuristic for deployment detection
			deployMeta.Name = controllerRef.Name
			if typeMetadata.Kind == "ReplicaSet" && pod.Labels["pod-template-hash"] != "" && strings.HasSuffix(controllerRef.Name, pod.Labels["pod-template-hash"]) {
				name := strings.TrimSuffix(controllerRef.Name, "-"+pod.Labels["pod-template-hash"])
				deployMeta.Name = name
				typeMetadata.Kind = "Deployment"
			} else if typeMetadata.Kind == "ReplicationController" && pod.Labels["deploymentconfig"] != "" {
				// If the pod is controlled by the replication controller, which is created by the DeploymentConfig resource in
				// Openshift platform, set the deploy name to the deployment config's name, and the kind to 'DeploymentConfig'.
				//
				// nolint: lll
				// For DeploymentConfig details, refer to
				// https://docs.openshift.com/container-platform/4.1/applications/deployments/what-deployments-are.html#deployments-and-deploymentconfigs_what-deployments-are
				//
				// For the reference to the pod label 'deploymentconfig', refer to
				// https://github.com/openshift/library-go/blob/7a65fdb398e28782ee1650959a5e0419121e97ae/pkg/apps/appsutil/const.go#L25
				deployMeta.Name = pod.Labels["deploymentconfig"]
				typeMetadata.Kind = "DeploymentConfig"
				delete(deployMeta.Labels, "deploymentconfig")
			} else if typeMetadata.Kind == "Job" {
				// If job name suffixed with `-<digit-timestamp>`, where the length of digit timestamp is 8~10,
				// trim the suffix and set kind to cron job.
				if jn := cronJobNameRegexp.FindStringSubmatch(controllerRef.Name); len(jn) == 2 {
					deployMeta.Name = jn[1]
					typeMetadata.Kind = "CronJob"
					// heuristically set cron job api version to v1beta1 as it cannot be derived from pod metadata.
					// Cronjob is not GA yet and latest version is v1beta1: https://github.com/kubernetes/enhancements/pull/978
					typeMetadata.APIVersion = "batch/v1beta1"
				}
			}
		}
	}

	if deployMeta.Name == "" {
		// if we haven't been able to extract a deployment name, then just give it the pod name
		deployMeta.Name = pod.Name
	}

	return deployMeta, typeMetadata
}

// MaxRequestBodyBytes represents the max size of Kubernetes objects we read. Kubernetes allows a 2x
// buffer on the max etcd size
// (https://github.com/kubernetes/kubernetes/blob/0afa569499d480df4977568454a50790891860f5/staging/src/k8s.io/apiserver/pkg/server/config.go#L362).
// We allow an additional 2x buffer, as it is still fairly cheap (6mb)
const MaxRequestBodyBytes = int64(6 * 1024 * 1024)

// HTTPConfigReader is reads an HTTP request, imposing size restrictions aligned with Kubernetes limits
func HTTPConfigReader(req *http.Request) ([]byte, error) {
	defer req.Body.Close()
	lr := &io.LimitedReader{
		R: req.Body,
		N: MaxRequestBodyBytes + 1,
	}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if lr.N <= 0 {
		return nil, errors.NewRequestEntityTooLargeError(fmt.Sprintf("limit is %d", MaxRequestBodyBytes))
	}
	return data, nil
}

// StripUnusedFields is the transform function for shared informers,
// it removes unused fields from objects before they are stored in the cache to save memory.
func StripUnusedFields(obj any) (any, error) {
	t, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		// shouldn't happen
		return obj, nil
	}
	// ManagedFields is large and we never use it
	t.GetObjectMeta().SetManagedFields(nil)
	return obj, nil
}

// StripNodeUnusedFields is the transform function for shared node informers,
// it removes unused fields from objects before they are stored in the cache to save memory.
func StripNodeUnusedFields(obj any) (any, error) {
	t, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		// shouldn't happen
		return obj, nil
	}
	// ManagedFields is large and we never use it
	t.GetObjectMeta().SetManagedFields(nil)
	// Annotation is never used
	t.GetObjectMeta().SetAnnotations(nil)
	// OwnerReference is never used
	t.GetObjectMeta().SetOwnerReferences(nil)
	// only node labels and addressed are useful
	if node := obj.(*corev1.Node); node != nil {
		node.Status.Allocatable = nil
		node.Status.Capacity = nil
		node.Status.Images = nil
		node.Status.Conditions = nil
	}

	return obj, nil
}

// StripPodUnusedFields is the transform function for shared pod informers,
// it removes unused fields from objects before they are stored in the cache to save memory.
func StripPodUnusedFields(obj any) (any, error) {
	t, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		// shouldn't happen
		return obj, nil
	}
	// ManagedFields is large and we never use it
	t.GetObjectMeta().SetManagedFields(nil)
	// only container ports can be used
	if pod := obj.(*corev1.Pod); pod != nil {
		containers := []corev1.Container{}
		for _, c := range pod.Spec.Containers {
			if len(c.Ports) > 0 {
				containers = append(containers, corev1.Container{
					Ports: c.Ports,
				})
			}
		}
		oldSpec := pod.Spec
		newSpec := corev1.PodSpec{
			Containers:         containers,
			ServiceAccountName: oldSpec.ServiceAccountName,
			NodeName:           oldSpec.NodeName,
			HostNetwork:        oldSpec.HostNetwork,
			Hostname:           oldSpec.Hostname,
			Subdomain:          oldSpec.Subdomain,
		}
		pod.Spec = newSpec
		pod.Status.InitContainerStatuses = nil
		pod.Status.ContainerStatuses = nil
	}

	return obj, nil
}

func SlowConvertKindsToRuntimeObjects(in []crd.IstioKind) ([]runtime.Object, error) {
	res := make([]runtime.Object, 0, len(in))
	for _, o := range in {
		r, err := SlowConvertToRuntimeObject(&o)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, nil
}

// SlowConvertToRuntimeObject converts an IstioKind to a runtime.Object.
// As the name implies, it is not efficient.
func SlowConvertToRuntimeObject(in *crd.IstioKind) (runtime.Object, error) {
	by, err := config.ToJSON(in)
	if err != nil {
		return nil, err
	}
	gvk := in.GetObjectKind().GroupVersionKind()
	obj, _, err := IstioCodec.UniversalDeserializer().Decode(by, &gvk, nil)
	if err != nil {
		return nil, err
	}
	return obj, nil
}
