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

package ambient

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/pkg/log"
)

func hasKubeRegistry(registries []string) bool {
	for _, r := range registries {
		if provider.ID(r) == provider.Kubernetes {
			return true
		}
	}
	return false
}

type ExecList struct {
	Cmd  string
	Args []string
}

func newExec(cmd string, args []string) *ExecList {
	return &ExecList{
		Cmd:  cmd,
		Args: args,
	}
}

func executeOutput(cmd string, args ...string) (string, error) {
	externalCommand := exec.Command(cmd, args...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	externalCommand.Stdout = stdout
	externalCommand.Stderr = stderr

	err := externalCommand.Run()

	if err != nil || len(stderr.Bytes()) != 0 {
		return stderr.String(), err
	}

	return strings.TrimSuffix(stdout.String(), "\n"), err
}

func execute(cmd string, args ...string) error {
	log.Debugf("Running command: %s %s", cmd, strings.Join(args, " "))
	externalCommand := exec.Command(cmd, args...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	externalCommand.Stdout = stdout
	externalCommand.Stderr = stderr

	err := externalCommand.Run()

	if len(stdout.String()) != 0 {
		log.Debugf("Command output: \n%v", stdout.String())
	}

	if err != nil || len(stderr.Bytes()) != 0 {
		log.Debugf("Command error output: \n%v", stderr.String())
		return errors.New(stderr.String())
	}

	return nil
}

// @TODO Interim function for pep, to be replaced after design meeting
func PodHasOptOut(pod *corev1.Pod) bool {
	if val, ok := pod.Labels["ambient-type"]; ok {
		return val == "pep" || val == "none"
	}
	return false
}

func (s *Server) matchesAmbientSelectors(lbl map[string]string) (bool, error) {
	sel, err := metav1.LabelSelectorAsSelector(&ambientSelectors)
	if err != nil {
		return false, fmt.Errorf("failed to parse ambient selectors: %v", err)
	}

	return sel.Matches(labels.Set(lbl)), nil
}

func (s *Server) matchesDisabledSelectors(lbl map[string]string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, selector := range s.disabledSelectors {
		sel, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return false, fmt.Errorf("failed to parse disabled selectors: %v", err)
		}
		if sel.Matches(labels.Set(lbl)) {
			return true, nil
		}
	}

	return false, nil
}

func HasSelectors(lbls map[string]string, selectors []*metav1.LabelSelector) bool {
	for _, selector := range selectors {
		sel, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			log.Errorf("Failed to parse selector: %v", err)
			return false
		}

		if sel.Matches(labels.Set(lbls)) {
			return true
		}
	}
	return false
}

// We do not support the istio.io/rev or istio-injection sidecar labels
// If a pod or namespace has these labels, ambient mesh will not be applied
// to that namespace
func HasLegacyLabel(lbl map[string]string) bool {
	for _, ls := range legacySelectors {
		sel, err := metav1.LabelSelectorAsSelector(ls)
		if err != nil {
			log.Errorf("Failed to parse legacy selector: %v", err)
			return false
		}

		if sel.Matches(labels.Set(lbl)) {
			return true
		}
	}

	return false
}

func podOnMyNode(pod *corev1.Pod) bool {
	return pod.Spec.NodeName == NodeName
}

func (s *Server) isAmbientGlobal() bool {
	return s.meshMode == v1alpha1.MeshConfig_AmbientMeshConfig_ON
}

func (s *Server) isAmbientNamespaced() bool {
	return s.meshMode == v1alpha1.MeshConfig_AmbientMeshConfig_DEFAULT
}

func (s *Server) isAmbientOff() bool {
	return s.meshMode == v1alpha1.MeshConfig_AmbientMeshConfig_OFF
}

func hasPodIP(pod *corev1.Pod) bool {
	return pod.Status.PodIP != ""
}

func isRunning(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning
}

func ShouldPodBeInIpset(namespace *corev1.Namespace, pod *corev1.Pod, meshMode string, ignoreNotRunning bool) bool {
	// Pod must:
	// - Be running
	// - Have an IP address
	// - Ambient mesh not be off
	// - Cannot have a legacy label (istio.io/rev or istio-injection=enabled)
	// - If mesh is in namespace mode, must be in active namespace
	if (ignoreNotRunning || (isRunning(pod) && hasPodIP(pod))) &&
		meshMode != AmbientMeshOff.String() &&
		!HasLegacyLabel(pod.GetLabels()) &&
		!PodHasOptOut(pod) &&
		isNamespaceActive(namespace, meshMode) {
		return true
	}

	return false
}

func isNamespaceActive(namespace *corev1.Namespace, meshMode string) bool {
	// Must:
	// - MeshConfig be in an "ON" mode
	// - MeshConfig must be in a "DEFAULT" mode, plus:
	//   - Namespace cannot have "legacy" labels (ie. istio.io/rev or istio-injection=enabled)
	//   - Namespace must have label istio.io/dataplane-mode=ambient
	if meshMode == AmbientMeshOn.String() ||
		(meshMode == AmbientMeshNamespace.String() &&
			!HasLegacyLabel(namespace.GetLabels()) &&
			namespace.GetLabels()["istio.io/dataplane-mode"] == "ambient") {
		return true
	}

	return false
}

func getEnvFromPod(pod *corev1.Pod, envName string) string {
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == envName {
				return env.Value
			}
		}
	}
	return ""
}
