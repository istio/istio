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

package common

const (
	// AnnotationKeySyncedAtVersion is used to tag destination resources with the resource version of
	// source.
	AnnotationKeySyncedAtVersion = "galley.istio.io/synced-at-version"

	// KubectlLastAppliedConfiguration is used by Kubernetes/kubectl. We remove them from the destination
	// resources to avoid confusing tools.
	KubectlLastAppliedConfiguration = "kubectl.kubernetes.io/last-applied-configuration"
)

// KnownAnnotations is as its name implies.
var KnownAnnotations = []string{AnnotationKeySyncedAtVersion, KubectlLastAppliedConfiguration}
