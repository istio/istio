// Copyright 2017 Istio Authors
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

package util

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
)

// Test utilities for kubernetes

// CreateNamespace creates a fresh namespace
func CreateNamespace(cl kubernetes.Interface) (string, error) {
	return CreateNamespaceWithPrefix(cl, "istio-test-", false)
}

// CreateNamespaceWithPrefix creates a fresh namespace with the given prefix
func CreateNamespaceWithPrefix(cl kubernetes.Interface, prefix string, inject bool) (string, error) {
	injectionValue := "disabled"
	if inject {
		injectionValue = "enabled"
	}
	ns, err := cl.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			GenerateName: prefix,
			Labels: map[string]string{
				"istio-injection": injectionValue,
			},
		},
	})
	if err != nil {
		return "", err
	}
	log.Infof("Created namespace %s", ns.Name)
	return ns.Name, nil
}

// DeleteNamespace removes a namespace
func DeleteNamespace(cl kubernetes.Interface, ns string) {
	if ns != "" && ns != "default" {
		if err := cl.CoreV1().Namespaces().Delete(ns, &meta_v1.DeleteOptions{}); err != nil {
			log.Warnf("Error deleting namespace: %v", err)
		}
		log.Infof("Deleted namespace %s", ns)
	}
}

// CopyFilesToPod copies files from a machine to a pod.
func CopyFilesToPod(container, pod, ns, source, dest string) error {
	// kubectl cp /tmp/bar  <some-namespace>/<some-pod>:/tmp/foo -c container
	cmd := fmt.Sprintf("kubectl cp %s %s/%s:%s", source, ns, pod, dest)
	if container != "" {
		cmd += " -c " + container
	}
	_, err := Shell(cmd)
	return err
}
