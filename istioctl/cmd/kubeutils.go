// Copyright Istio Authors.
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

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"istio.io/pkg/env"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// k8sClientConfig first tries to get the config from inside a kubernetes environment
// If unsuccessful, it tries to get the config from the default kubeconfig file
func k8sClientConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	var ocErr error
	if err != nil {
		config, ocErr = clientcmd.BuildConfigFromFlags("", kubeconfigPath())
		if ocErr != nil {
			return nil, fmt.Errorf("in-cluster error: %v, out-of-cluster error: %v", err, ocErr)
		}
	}
	return config, nil
}

// Returns a kubernetes clientset with the default config
func k8sClientset() (*kubernetes.Clientset, error) {
	config, err := k8sClientConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// Returns a kubernetes service in a namespace
func k8sService(name, namespace string) (*v1.Service, error) {
	clientset, err := k8sClientset()
	if err != nil {
		return nil, err
	}
	service, err := clientset.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

// Returns a kubernetes pod in a namespace
func k8sPod(name, namespace string) (*v1.Pod, error) {
	clientset, err := k8sClientset()
	if err != nil {
		return nil, err
	}
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

// Execs a command inside a kubernetes pod and returns stdout and stderr
func k8sPodsExec(name, namespace, command string, stdin io.Reader) (sout, serr string, err error) {
	pod, err := k8sPod(name, namespace)
	if err != nil {
		return
	}
	clientset, err := k8sClientset()
	if err != nil {
		return
	}
	req := clientset.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Command: strings.Fields(command),
		Stdin:   stdin != nil,
		Stderr:  true,
		Stdout:  true,
	}, scheme.ParameterCodec)

	config, err := k8sClientConfig()
	if err != nil {
		return
	}
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

// Returns the kubectl config
func k8sConfig() *api.Config {
	return clientcmd.GetConfigFromFileOrDie(kubeconfigPath())
}

func kubeconfigPath() string {
	if home := homeDir(); home != "" {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}

func homeDir() string {
	if home := env.RegisterStringVar("HOME", "", "Linux default home directory").Get(); home != "" {
		return home
	}
	return env.RegisterStringVar("USERPROFILE", "", "Windows default home directory").Get()
}
