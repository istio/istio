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

package handlers

import (
	"strings"

	v1 "k8s.io/api/core/v1"
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
