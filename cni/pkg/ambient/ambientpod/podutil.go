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

package ambientpod

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/pkg/log"
)

// PodZtunnelEnabled determines if a pod is eligible for ztunnel redirection
func PodZtunnelEnabled(namespace *corev1.Namespace, pod *corev1.Pod) bool {
	if namespace.GetLabels()[constants.DataplaneMode] != constants.DataplaneModeAmbient {
		// Namespace does not have ambient mode enabled
		return false
	}
	if podHasSidecar(pod) {
		// Ztunnel and sidecar for a single pod is currently not supported; opt out.
		return false
	}
	if pod.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionDisabled {
		// Pod explicitly asked to not have redirection enabled
		return false
	}
	return true
}

func podHasSidecar(pod *corev1.Pod) bool {
	if _, f := pod.Annotations[annotation.SidecarStatus.Name]; f {
		return true
	}
	return false
}

var LegacyLabelSelector = []*metav1.LabelSelector{
	{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "istio-injection",
				Operator: metav1.LabelSelectorOpIn,
				Values: []string{
					"enabled",
				},
			},
		},
	},
	{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      label.IoIstioRev.Name,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	},
}

var LegacySelectors = ConvertDisabledSelectors(LegacyLabelSelector)

func ConvertDisabledSelectors(selectors []*metav1.LabelSelector) []labels.Selector {
	res := make([]labels.Selector, 0, len(selectors))
	for _, k := range selectors {
		s, err := metav1.LabelSelectorAsSelector(k)
		if err != nil {
			log.Errorf("failed to convert label selector: %v", err)
			continue
		}
		res = append(res, s)
	}
	return res
}

// We do not support the istio.io/rev or istio-injection sidecar labels
// If a pod or namespace has these labels, ambient mesh will not be applied
// to that namespace
func HasLegacyLabel(lbl map[string]string) bool {
	for _, sel := range LegacySelectors {
		if sel.Matches(labels.Set(lbl)) {
			return true
		}
	}

	return false
}
