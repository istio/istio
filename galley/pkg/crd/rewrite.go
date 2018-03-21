//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package crd

import (
	"strings"

	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"

	"istio.io/istio/galley/pkg/common"
	"istio.io/istio/pkg/log"
)

// rewrite the given source CRD as a destination CRD with the given group/version.
func rewrite(
	source *ext.CustomResourceDefinition,
	group string,
	version string) *ext.CustomResourceDefinition {

	modified := source.DeepCopy()

	annotations := modified.GetAnnotations()

	if annotations != nil {
		delete(annotations, common.KubectlLastAppliedConfiguration)
	} else {
		annotations = make(map[string]string, 1)
	}
	annotations[common.AnnotationKeySyncedAtVersion] = source.ObjectMeta.ResourceVersion

	modified.GetObjectMeta().SetAnnotations(annotations)
	modified.Spec.Group = group
	modified.Spec.Version = version
	modified.Name = rewriteName(group, source.Name)
	modified.ResourceVersion = ""
	modified.UID = ""

	log.Debugf("CRD Rewrite: %s -> %s", source.Name, modified.Name)
	return modified
}

func rewriteName(groupName string, name string) string {
	prefix := strings.Split(name, ".")[0]
	return prefix + "." + groupName
}
