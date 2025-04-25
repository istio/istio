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

package util

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
)

var annotationEnabledPatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":"%s"}}}`,
	annotation.AmbientRedirection.Name,
	constants.AmbientRedirectionEnabled,
))

var annotationPendingPatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":"%s"}}}`,
	annotation.AmbientRedirection.Name,
	constants.AmbientRedirectionPending,
))

var annotationRemovePatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":null}}}`,
	annotation.AmbientRedirection.Name,
))

// PodRedirectionEnabled determines if a pod should or should not be configured
// to have traffic redirected thru the node proxy.
// func PodRedirectionEnabled(namespace *corev1.Namespace, pod *corev1.Pod, enablementSelector labels.Selector) bool {
// 	nsMatches := enablementSelector.Matches(labels.Set(namespace.GetLabels()))
// 	podMatches := enablementSelector.Matches(labels.Set(pod.GetLabels()))
// 	if !(nsMatches || podMatches) {
// 		// Neither namespace nor pod has ambient mode enabled
// 		return false
// 	}
// 	if podHasSidecar(pod) {
// 		// Ztunnel and sidecar for a single pod is currently not supported; opt out.
// 		return false
// 	}
// 	// TODO (therealmitchconnors): how to accomplish this with the enablement selector?
// 	reqs, _ := enablementSelector.Requirements()
// 	if len(reqs) == 1 {
// 		_, explicitPodLabeled := pod.GetLabels()[reqs[0].Key()]
// 		if explicitPodLabeled && !podMatches {
// 			// Pod explicitly asked to not have ambient redirection enabled
// 			return false
// 		}
// 	}
// 	if pod.Spec.HostNetwork {
// 		// Host network pods cannot be captured, as we require inserting rules into the pod network namespace.
// 		// If we were to allow them, we would be writing these rules into the host network namespace, effectively breaking the host.
// 		return false
// 	}
// 	return true
// }

// PodFullyEnrolled reports on whether the pod _has_ actually been fully configured for traffic redirection.
//
// That is, have we annotated it after successfully setting up iptables rules AND sending it to a node proxy instance.
//
// If you just want to know if the pod _should be_ configured for traffic redirection, see PodRedirectionEnabled
func PodFullyEnrolled(pod *corev1.Pod) bool {
	if pod != nil {
		return pod.GetAnnotations()[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionEnabled
	}
	return false
}

// PodPartiallyEnrolled reports on whether the pod _has_ already been partially configured
// (e.g. for traffic redirection) but not fully configured.
//
// That is, have we annotated it after setting iptables rules, but have not yet been able to send it to
// a node proxy instance.
//
// Pods like this still need to undergo the removal process (to potentially undo the redirection).
//
// If you just want to know if the pod _should be_ configured for traffic redirection, see PodRedirectionEnabled
func PodPartiallyEnrolled(pod *corev1.Pod) bool {
	if pod != nil {
		return pod.GetAnnotations()[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionPending
	}
	return false
}

func podHasSidecar(podAnnotations map[string]string) bool {
	if _, f := podAnnotations[annotation.SidecarStatus.Name]; f {
		return true
	}
	return false
}

func IsZtunnelPod(systemNs string, pod *corev1.Pod) bool {
	return pod.Namespace == systemNs && pod.GetLabels()["app"] == "ztunnel"
}

func AnnotateEnrolledPod(client kubernetes.Interface, pod *metav1.ObjectMeta) error {
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationEnabledPatch,
			metav1.PatchOptions{},
			// Both "pods" and "pods/status" can mutate the metadata. However, pods/status is lower privilege, so we use that instead.
			"status",
		)
	return err
}

func AnnotatePartiallyEnrolledPod(client kubernetes.Interface, pod *metav1.ObjectMeta) error {
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationPendingPatch,
			metav1.PatchOptions{},
			// Both "pods" and "pods/status" can mutate the metadata. However, pods/status is lower privilege, so we use that instead.
			"status",
		)
	return err
}

func AnnotateUnenrollPod(client kubernetes.Interface, pod *metav1.ObjectMeta) error {
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationRemovePatch,
			metav1.PatchOptions{},
			// Both "pods" and "pods/status" can mutate the metadata. However, pods/status is lower privilege, so we use that instead.
			"status",
		)
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// Get any IPs currently assigned to the Pod.
//
// If 'PodIPs' exists, it is preferred (and should be guaranteed to contain the address in 'PodIP'),
// otherwise fallback to 'PodIP'.
//
// Note that very early in the pod's lifecycle (before all the node CNI plugin invocations finish)
// K8S may not have received the pod IPs yet, and may not report the pod as having any.
func GetPodIPsIfPresent(pod *corev1.Pod) []netip.Addr {
	var podIPs []netip.Addr
	if len(pod.Status.PodIPs) != 0 {
		for _, pip := range pod.Status.PodIPs {
			ip := netip.MustParseAddr(pip.IP)
			podIPs = append(podIPs, ip)
		}
	} else if len(pod.Status.PodIP) != 0 {
		ip := netip.MustParseAddr(pod.Status.PodIP)
		podIPs = append(podIPs, ip)
	}
	return podIPs
}

// CheckBooleanAnnotation checks for the named boolean-style (as per strcov.ParseBool)
// annotation on the pod. Returns the bool value, and a bool indicating annotation presence
// on the pod.
//
// If the bool value is false, not present, or unparsable, returns a false value.
// If the annotation is not present or unparsable, returns false for presence.
func CheckBooleanAnnotation(pod *corev1.Pod, annotationName string) (bool, bool) {
	val, isPresent := pod.Annotations[annotationName]

	if isPresent {
		var err error
		parsedVal, err := strconv.ParseBool(val)
		if err != nil {
			log.Warnf("annotation %v=%q found, but only boolean values are supported, ignoring", annotationName, val)
			return false, false
		}
		return parsedVal, isPresent
	}

	return false, false
}
