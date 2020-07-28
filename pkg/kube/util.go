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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"istio.io/pkg/env"

	kubeApiCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // allow out of cluster authentication
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/remotecommand"
)

// BuildClientConfig builds a client rest config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	return BuildClientCmd(kubeconfig, context).ClientConfig()
}

// BuildClientCmd builds a client cmd config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientCmd(kubeconfig, context string) clientcmd.ClientConfig {
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

// SetRestDefaults is a helper function that sets default values for the given rest.Config.
func SetRestDefaults(config *rest.Config) *rest.Config {
	if config.GroupVersion == nil || config.GroupVersion.Empty() {
		config.GroupVersion = &kubeApiCore.SchemeGroupVersion
	}
	if len(config.APIPath) == 0 {
		if len(config.GroupVersion.Group) == 0 {
			config.APIPath = "/api"
		} else {
			config.APIPath = "/apis"
		}
	}
	if len(config.ContentType) == 0 {
		config.ContentType = runtime.ContentTypeJSON
	}
	if config.NegotiatedSerializer == nil {
		// This codec factory ensures the resources are not converted. Therefore, resources
		// will not be round-tripped through internal versions. Defaulting does not happen
		// on the client.
		config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	}
	if len(config.UserAgent) == 0 {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	return config
}

// CheckPodReady returns nil if the given pod and all of its containers are ready.
func CheckPodReady(pod *kubeApiCore.Pod) error {
	switch pod.Status.Phase {
	case kubeApiCore.PodSucceeded:
		return nil
	case kubeApiCore.PodRunning:
		// Wait until all containers are ready.
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return fmt.Errorf("container not ready: '%s'", containerStatus.Name)
			}
		}
		if len(pod.Status.Conditions) > 0 {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == kubeApiCore.PodReady && condition.Status != kubeApiCore.ConditionTrue {
					return fmt.Errorf("pod not ready, condition message: %v", condition.Message)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

// ClientConfig first tries to get the config from inside a kubernetes environment
// If unsuccessful, it tries to get the config from the default kubeconfig file
func ClientConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	var ocErr error
	if err != nil {
		config, ocErr = clientcmd.BuildConfigFromFlags("", defaultKubeconfigPath())
		if ocErr != nil {
			return nil, fmt.Errorf("in-cluster error: %v, out-of-cluster error: %v", err, ocErr)
		}
	}
	return config, nil
}

// Returns a kubernetes clientset with the default config
// TODO: align and remove redundancy with BuildClientConfig, CreateClientset
// as well as istio.io/istio/operator/cmd/mesh/shared.go, and potentially istio.io/istio/cni/cmd/istio-cni/kubernetes.go
func Config() (*rest.Config, *kubernetes.Clientset, error) {
	config, err := ClientConfig()
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return config, clientset, nil
}

// Returns a kubernetes service in a namespace
// TODO: remove once merged
// nolint: deadcode
func Service(clientset *kubernetes.Clientset, name, namespace string) (*kubeApiCore.Service, error) {
	service, err := clientset.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

// Returns a kubernetes pod in a namespace
func Pod(clientset *kubernetes.Clientset, name, namespace string) (*kubeApiCore.Pod, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if strings.Contains(pod.Name, name) {
			return &pod, nil
		}
	}
	return nil, fmt.Errorf("pod with name %s not found in namespace %s", name, namespace)
}

// Returns a kubernetes config map in a namespace
// TODO: remove once merged
// nolint: deadcode
func ConfigMap(clientset *kubernetes.Clientset, name, namespace string) (*kubeApiCore.ConfigMap, error) {
	cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm, nil
}

// Execs a command inside a kubernetes pod and returns stdout and stderr
// nolint: unparam
func PodExec(config *rest.Config, clientset *kubernetes.Clientset, name, namespace, command string, stdin io.Reader) (sout, serr string, err error) {
	pod, err := Pod(clientset, name, namespace)
	if err != nil {
		return
	}
	req := clientset.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec")
	req.VersionedParams(&kubeApiCore.PodExecOptions{
		Command: strings.Fields(command),
		Stdin:   stdin != nil,
		Stderr:  true,
		Stdout:  true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return
	}
	return stdout.String(), stderr.String(), nil
}

// Returns the kubectl config from the given kubeconfig path
// Uses the default kubeconfig path if the given path is empty
func Kubeconfig(kubeconfigPath string) *api.Config {
	if kubeconfigPath == "" {
		kubeconfigPath = defaultKubeconfigPath()
	}
	return clientcmd.GetConfigFromFileOrDie(kubeconfigPath)
}

func defaultKubeconfigPath() string {
	home := env.RegisterStringVar("HOME", "", "Linux default home directory").Get()
	if home == "" {
		return env.RegisterStringVar("USERPROFILE", "", "Windows default home directory").Get()
	}
	if home != "" {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}
