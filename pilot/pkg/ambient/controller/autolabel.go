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

package controller

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/features"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/env"
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

	ignored := sets.New(append(strings.Split(features.SidecarlessAutolabelIgnore, ","), opts.SystemNamespace)...)
	workloadHandler := controllers.FilteredObjectHandler(queue.AddObject, ambientLabelFilter(ignored))
	opts.Client.KubeInformer().Core().V1().Pods().Informer().AddEventHandler(workloadHandler)
	go queue.Run(opts.Stop)
}

var labelPatch = []byte(fmt.Sprintf(
	`[{"op":"add","path":"/metadata/labels/%s","value":"%s" },{"op":"add","path":"/metadata/labels/%s","value":"%s" }]`,
	ambient.LabelType,
	ambient.TypeWorkload,
	ambient.LegacyLabelType,
	ambient.TypeWorkload,
))

func ambientLabelFilter(ignoredNamespaces sets.Set) func(o controllers.Object) bool {
	return func(o controllers.Object) bool {
		_, alreadyLabelled := o.GetLabels()[ambient.LabelType] // PEPs uProxies will already be labeled
		ignored := inject.IgnoredNamespaces.Contains(o.GetNamespace()) || ignoredNamespaces.Contains(o.GetNamespace())
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
