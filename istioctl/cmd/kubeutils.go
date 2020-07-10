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
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Returns a kubernetes clientset from outside the cluster
func outsideClient() *kubernetes.Clientset {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

// Returns a service in the istio-system namespace
// TODO: deal with nonexistent clusters, only errs when namespace is nonexistent
func istioService(clientset *kubernetes.Clientset, serviceName string) (*v1.Service, error) {
	service, err := clientset.CoreV1().Services("istio-system").Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	} else if service.Name == "" {
		return nil, fmt.Errorf("fervice %s does not exist", serviceName)
	}
	return service, nil
}

// TODO: Get project, location, cluster from local configuration
func localConfig(clientset *kubernetes.Clientset) (string, string, string) {
	project := "justinwei-test-1"
	location := "us-central1-c"
	cluster := "cluster-api-single"
	return project, location, cluster
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	// Case for Windows
	return os.Getenv("USERPROFILE")
}
