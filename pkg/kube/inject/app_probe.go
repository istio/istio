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

// Package inject implements kube-inject or webhoook autoinject feature to inject sidecar.
// This file is focused on rewriting Kubernetes app probers to support mutual TLS.
package inject

import (
	"encoding/json"
	"fmt"
	"strconv"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ShouldRewriteAppHTTPProbers returns if we should rewrite apps' probers config.
func ShouldRewriteAppHTTPProbers(annotations map[string]string, spec *SidecarInjectionSpec) bool {
	if annotations != nil {
		if value, ok := annotations[annotation.SidecarRewriteAppHTTPProbers.Name]; ok {
			if isSetInAnnotation, err := strconv.ParseBool(value); err == nil {
				return isSetInAnnotation
			}
		}
	}
	if spec == nil {
		return false
	}
	return spec.RewriteAppHTTPProbe
}

// FindSidecar returns the pointer to the first container whose name matches the "istio-proxy".
func FindSidecar(containers []corev1.Container) *corev1.Container {
	for i := range containers {
		if containers[i].Name == ProxyContainerName {
			return &containers[i]
		}
	}
	return nil
}

// convertAppProber returns an overwritten `Probe` for pilot agent to take over.
func convertAppProber(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	if probe == nil || probe.HTTPGet == nil {
		return nil
	}
	p := probe.DeepCopy()
	// Change the application container prober config.
	p.HTTPGet.Port = intstr.FromInt(statusPort)
	p.HTTPGet.Path = newURL
	// For HTTPS prober, we change to HTTP,
	// and pilot agent uses https to request application prober endpoint.
	// Kubelet -> HTTP -> Pilot Agent -> HTTPS -> Application
	if p.HTTPGet.Scheme == corev1.URISchemeHTTPS {
		p.HTTPGet.Scheme = corev1.URISchemeHTTP
	}
	return p
}

// DumpAppProbers returns a json encoded string as `status.KubeAppProbers`.
// Also update the probers so that all usages of named port will be resolved to integer.
func DumpAppProbers(podspec *corev1.PodSpec) string {
	out := status.KubeAppProbers{}
	updateNamedPort := func(p *status.Prober, portMap map[string]int32) *status.Prober {
		if p == nil || p.HTTPGet == nil {
			return nil
		}
		if p.HTTPGet.Port.Type == intstr.String {
			port, exists := portMap[p.HTTPGet.Port.StrVal]
			if !exists {
				return nil
			}
			p.HTTPGet.Port = intstr.FromInt(int(port))
		}
		return p
	}
	for _, c := range podspec.Containers {
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez, startupz := status.FormatProberURL(c.Name)
		portMap := map[string]int32{}
		for _, p := range c.Ports {
			if p.Name != "" {
				portMap[p.Name] = p.ContainerPort
			}
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.ReadinessProbe), portMap); h != nil {
			out[readyz] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.LivenessProbe), portMap); h != nil {
			out[livez] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.StartupProbe), portMap); h != nil {
			out[startupz] = h
		}

	}
	b, err := json.Marshal(out)
	if err != nil {
		log.Errorf("failed to serialize the app prober config %v", err)
		return ""
	}
	return string(b)
}

// rewriteAppHTTPProbes modifies the app probers in place for kube-inject.
func rewriteAppHTTPProbe(annotations map[string]string, podSpec *corev1.PodSpec, spec *SidecarInjectionSpec, port int32) {

	if !ShouldRewriteAppHTTPProbers(annotations, spec) {
		return
	}
	sidecar := FindSidecar(podSpec.Containers)
	if sidecar == nil {
		return
	}

	statusPort := int(port)
	if v, f := annotations[annotation.SidecarStatusPort.Name]; f {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Errorf("Invalid annotation %v=%v: %v", annotation.SidecarStatusPort, p, err)
		}
		statusPort = p
	}
	// Pilot agent statusPort is not defined, skip changing application http probe.
	if statusPort == -1 {
		return
	}
	if prober := DumpAppProbers(podSpec); prober != "" {
		// We don't have to escape json encoding here when using golang libraries.
		sidecar.Env = append(sidecar.Env, corev1.EnvVar{Name: status.KubeAppProberEnvName, Value: prober})
	}
	// Now modify the container probers.
	for _, c := range podSpec.Containers {
		// Skip sidecar container.
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez, startupz := status.FormatProberURL(c.Name)
		if rp := convertAppProber(c.ReadinessProbe, readyz, statusPort); rp != nil {
			*c.ReadinessProbe = *rp
		}
		if lp := convertAppProber(c.LivenessProbe, livez, statusPort); lp != nil {
			*c.LivenessProbe = *lp
		}
		if sp := convertAppProber(c.StartupProbe, startupz, statusPort); sp != nil {
			*c.StartupProbe = *sp
		}
	}
}

// createProbeRewritePatch generates the patch for webhook.
func createProbeRewritePatch(annotations map[string]string, podSpec *corev1.PodSpec, spec *SidecarInjectionSpec, defaultPort int32) []rfc6902PatchOperation {
	if !ShouldRewriteAppHTTPProbers(annotations, spec) {
		return []rfc6902PatchOperation{}
	}
	podPatches := []rfc6902PatchOperation{}
	sidecar := FindSidecar(spec.Containers)
	if sidecar == nil {
		return nil
	}
	statusPort := int(defaultPort)
	if v, f := annotations[annotation.SidecarStatusPort.Name]; f {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Errorf("Invalid annotation %v=%v: %v", annotation.SidecarStatusPort, p, err)
		}
		statusPort = p
	}
	for i, c := range podSpec.Containers {
		// Skip sidecar container.
		if c.Name == ProxyContainerName {
			continue
		}
		portMap := map[string]int32{}
		for _, p := range c.Ports {
			portMap[p.Name] = p.ContainerPort
		}
		readyz, livez, startupz := status.FormatProberURL(c.Name)
		if probePatch := convertAppProber(c.ReadinessProbe, readyz, statusPort); probePatch != nil {
			podPatches = append(podPatches, rfc6902PatchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%v/readinessProbe", i),
				Value: *probePatch,
			})
		}
		if probePatch := convertAppProber(c.LivenessProbe, livez, statusPort); probePatch != nil {
			podPatches = append(podPatches, rfc6902PatchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%v/livenessProbe", i),
				Value: *probePatch,
			})
		}
		if probePatch := convertAppProber(c.StartupProbe, startupz, statusPort); probePatch != nil {
			podPatches = append(podPatches, rfc6902PatchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%v/startupProbe", i),
				Value: *probePatch,
			})
		}
	}
	return podPatches
}

// kubeProbeToInternalProber converts a Kubernetes Probe to an Istio internal Prober
func kubeProbeToInternalProber(probe *corev1.Probe) *status.Prober {
	if probe == nil {
		return nil
	}

	if probe.HTTPGet == nil {
		return nil
	}

	return &status.Prober{
		HTTPGet:        probe.HTTPGet,
		TimeoutSeconds: probe.TimeoutSeconds,
	}
}
