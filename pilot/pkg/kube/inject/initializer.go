// Copyright 2018 Istio Authors
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
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/batch/v2alpha1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var ignoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

var (
	kinds = []struct {
		groupVersion schema.GroupVersion
		obj          runtime.Object
		resource     string
		apiPath      string
	}{
		{v1.SchemeGroupVersion, &v1.ReplicationController{}, "replicationcontrollers", "/api"},
		{v1.SchemeGroupVersion, &v1.Pod{}, "pods", "/api"},

		{v1beta1.SchemeGroupVersion, &v1beta1.Deployment{}, "deployments", "/apis"},
		{v1beta1.SchemeGroupVersion, &v1beta1.DaemonSet{}, "daemonsets", "/apis"},
		{v1beta1.SchemeGroupVersion, &v1beta1.ReplicaSet{}, "replicasets", "/apis"},

		{batchv1.SchemeGroupVersion, &batchv1.Job{}, "jobs", "/apis"},
		{v2alpha1.SchemeGroupVersion, &v2alpha1.CronJob{}, "cronjobs", "/apis"},

		{appsv1beta1.SchemeGroupVersion, &appsv1beta1.StatefulSet{}, "statefulsets", "/apis"},

		{v1.SchemeGroupVersion, &v1.List{}, "lists", "/apis"},

		{openshiftv1.SchemeGroupVersion, &openshiftv1.DeploymentConfig{}, "deploymentconfigs", "/apis"},
		{openshiftv1.LegacySchemeGroupVersion, &openshiftv1.DeploymentConfig{}, "deploymentconfigs", "/apis"},
	}
	injectScheme = runtime.NewScheme()
)

func init() {
	for _, kind := range kinds {
		injectScheme.AddKnownTypes(kind.groupVersion, kind.obj)
		injectScheme.AddUnversionedTypes(kind.groupVersion, kind.obj)
	}
}
