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

package inject

import (
	openshiftv1 "github.com/openshift/api/apps/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/util/sets"
)

// IgnoredNamespaces contains the system namespaces referenced from Kubernetes:
// Ref: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/#viewing-namespaces
// "kube-system": The namespace for objects created by the Kubernetes system.
// "kube-public": This namespace is mostly reserved for cluster usage.
// "kube-node-lease": This namespace for the lease objects associated with each node
//    which improves the performance of the node heartbeats as the cluster scales.
// "local-path-storage": Dynamically provisioning persistent local storage with Kubernetes.
//    used with Kind cluster: https://github.com/rancher/local-path-provisioner
var IgnoredNamespaces = sets.New(
	constants.KubeSystemNamespace,
	constants.KubePublicNamespace,
	constants.KubeNodeLeaseNamespace,
	constants.LocalPathStorageNamespace)

var (
	kinds = []struct {
		groupVersion schema.GroupVersion
		obj          runtime.Object
		resource     string
		apiPath      string
	}{
		{v1.SchemeGroupVersion, &v1.ReplicationController{}, "replicationcontrollers", "/api"},
		{v1.SchemeGroupVersion, &v1.Pod{}, "pods", "/api"},

		{appsv1.SchemeGroupVersion, &appsv1.Deployment{}, "deployments", "/apis"},
		{appsv1.SchemeGroupVersion, &appsv1.DaemonSet{}, "daemonsets", "/apis"},
		{appsv1.SchemeGroupVersion, &appsv1.ReplicaSet{}, "replicasets", "/apis"},

		{batchv1.SchemeGroupVersion, &batchv1.Job{}, "jobs", "/apis"},
		{batchv1.SchemeGroupVersion, &batchv1.CronJob{}, "cronjobs", "/apis"},

		{appsv1.SchemeGroupVersion, &appsv1.StatefulSet{}, "statefulsets", "/apis"},

		{v1.SchemeGroupVersion, &v1.List{}, "lists", "/apis"},

		{openshiftv1.GroupVersion, &openshiftv1.DeploymentConfig{}, "deploymentconfigs", "/apis"},
	}
	injectScheme = runtime.NewScheme()
)

func init() {
	for _, kind := range kinds {
		injectScheme.AddKnownTypes(kind.groupVersion, kind.obj)
		injectScheme.AddUnversionedTypes(kind.groupVersion, kind.obj)
	}
}
