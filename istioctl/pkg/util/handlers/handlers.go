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

package handlers

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

//InferPodInfo Uses proxyName to infer namespace if the passed proxyName contains namespace information.
// Otherwise uses the namespace value passed into the function
func InferPodInfo(proxyName, namespace string) (string, string) {
	separator := strings.LastIndex(proxyName, ".")
	if separator < 0 {
		return proxyName, namespace
	}

	return proxyName[0:separator], proxyName[separator+1:]
}

//HandleNamespace returns the defaultNamespace if the namespace is empty
func HandleNamespace(ns, defaultNamespace string) string {
	if ns == v1.NamespaceAll {
		ns = defaultNamespace
	}
	return ns
}

//GetDefaultNamespace returns the default namespace for a kubeconfig
func GetDefaultNamespace(kubeconfig string) string {
	configAccess := clientcmd.NewDefaultPathOptions()

	if kubeconfig != "" {
		// use specified kubeconfig file for the location of the
		// config to read
		configAccess.GlobalFile = kubeconfig
	}

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		return v1.NamespaceDefault
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return v1.NamespaceDefault
	}
	if context.Namespace == "" {
		return v1.NamespaceDefault
	}
	return context.Namespace
}

//HandleNamespaces returns the correct namespace
func HandleNamespaces(objectNamespace, namespace, defaultNamespace string) (string, error) {
	if objectNamespace != "" && namespace != "" && namespace != objectNamespace {
		return "", fmt.Errorf(`the namespace from the provided object "%s" does `+
			`not match the namespace "%s". You must pass '--namespace=%s' to perform `+
			`this operation`, objectNamespace, namespace, objectNamespace)
	}

	if namespace != "" {
		return namespace, nil
	}

	if objectNamespace != "" {
		return objectNamespace, nil
	}
	return defaultNamespace, nil
}
