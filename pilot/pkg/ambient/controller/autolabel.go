package controller

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

import (
	"context"
	"fmt"
	"istio.io/istio/pilot/pkg/ambient"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/env"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var autoLabel = env.RegisterBoolVar("AMBIENT_AUTO_LABEL", true, "").Get()

func initAutolabel(opts *Options) {
	if !autoLabel {
		return
	}
	log.Infof("Starting ambient mesh auto-labeler")

	queue := controllers.NewQueue("ambient label controller",
		controllers.WithReconciler(ambientLabelPatcher(opts.Client)),
		controllers.WithMaxAttempts(5),
	)

	workloadHandler := controllers.FilteredObjectHandler(queue.AddObject, ambientLabelFilter(opts.SystemNamespace))
	opts.Client.KubeInformer().Core().V1().Pods().Informer().AddEventHandler(workloadHandler)
	go queue.Run(opts.Stop)
}

var labelPatch = []byte(fmt.Sprintf(`[{"op":"add","path":"/metadata/labels/%s","value":"%s" }]`, ambient.LabelType, ambient.TypeWorkload))

func ambientLabelFilter(systemNamespace string) func(o controllers.Object) bool {
	return func(o controllers.Object) bool {
		_, alreadyLabelled := o.GetLabels()[ambient.LabelType] // PEPs uProxies will already be labeled
		ignored := inject.IgnoredNamespaces.Contains(o.GetNamespace()) || o.GetNamespace() == systemNamespace
		return !alreadyLabelled && !ignored
	}
}
func ambientLabelPatcher(client kubelib.Client) func(types.NamespacedName) error {
	return func(key types.NamespacedName) error {
		_, err := client.CoreV1().
			Pods(key.Namespace).
			Patch(
				context.Background(),
				key.Name,
				types.JSONPatchType,
				labelPatch, metav1.PatchOptions{},
			)
		return err
	}
}
