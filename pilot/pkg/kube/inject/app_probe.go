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

// Package inject implements kube-inject or webhoook autoinject feature to inject sidecar.
// This file is focused on rewriting Kubernetes app probers to support mutual TLS.
package inject

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pkg/log"
)

const (
	// StatusPortCmdFlagName is the name of the command line flag passed to pilot-agent for sidecar readiness probe.
	// We reuse it for taking over application's readiness probing as well.
	// TODO: replace the hardcoded statusPort elsewhere by this variable as much as possible.
	StatusPortCmdFlagName = "statusPort"
)

var (
	// regex pattern for to extract the pilot agent probing port.
	// Supported format, --statusPort, -statusPort, --statusPort=15020.
	statusPortPattern = regexp.MustCompile(fmt.Sprintf(`^-{1,2}%s(=(?P<port>\d+))?$`, StatusPortCmdFlagName))
)

// ShouldRewriteAppProbers returns if we should rewrite apps' probers config.
func ShouldRewriteAppProbers(spec *SidecarInjectionSpec) bool {
	if spec == nil {
		return false
	}
	if !spec.RewriteAppHTTPProbe {
		return false
	}
	// TODO: check statusPort is defined, sidecar exists, per deployment annotation, etc.
	return true
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

// extractStatusPort accepts the sidecar container spec and returns its port for healthiness probing.
func extractStatusPort(sidecar *corev1.Container) int {
	for i, arg := range sidecar.Args {
		// Skip for unrelated args.
		match := statusPortPattern.FindAllStringSubmatch(strings.TrimSpace(arg), -1)
		if len(match) != 1 {
			continue
		}
		groups := statusPortPattern.SubexpNames()
		portStr := ""
		for ind, s := range match[0] {
			if groups[ind] == "port" {
				portStr = s
				break
			}
		}
		// Port not found from current arg, extract from next arg.
		if portStr == "" {
			// Matches the regex pattern, but without actual values provided.
			if len(sidecar.Args) <= i+1 {
				log.Errorf("No statusPort value provided, skip app probe rewriting")
				return -1
			}
			portStr = sidecar.Args[i+1]
		}
		p, err := strconv.Atoi(portStr)
		if err != nil {
			log.Errorf("Failed to convert statusPort to int %v, err %v", portStr, err)
			return -1
		}
		return p
	}
	return -1
}

// convertAppProber returns a overwritten `HTTPGetAction` for pilot agent to take over.
func convertAppProber(probe *corev1.Probe, newURL string, statusPort int) *corev1.HTTPGetAction {
	if probe == nil || probe.HTTPGet == nil {
		return nil
	}
	c := proto.Clone(probe.HTTPGet).(*corev1.HTTPGetAction)
	// Change the application container prober config.
	c.Port = intstr.FromInt(statusPort)
	c.Path = newURL
	return c
}

// DumpAppProbers returns a json encoded string as `status.KubeAppProbers`.
// Also update the probers so that all usages of named port will be resolved to integer.
func DumpAppProbers(podspec *corev1.PodSpec) string {
	out := status.KubeAppProbers{}
	updateNamedPort := func(p *corev1.Probe, portMap map[string]int32) *corev1.HTTPGetAction {
		if p == nil || p.HTTPGet == nil {
			return nil
		}
		h := p.HTTPGet
		if h.Port.Type == intstr.String {
			port, exists := portMap[h.Port.StrVal]
			if !exists {
				return nil
			}
			h.Port = intstr.FromInt(int(port))
		}
		return h
	}
	for _, c := range podspec.Containers {
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez := status.FormatProberURL(c.Name)
		portMap := map[string]int32{}
		for _, p := range c.Ports {
			if p.Name != "" {
				portMap[p.Name] = p.ContainerPort
			}
		}
		if h := updateNamedPort(c.ReadinessProbe, portMap); h != nil {
			out[readyz] = h
		}
		if h := updateNamedPort(c.LivenessProbe, portMap); h != nil {
			out[livez] = h
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
func rewriteAppHTTPProbe(podSpec *corev1.PodSpec, spec *SidecarInjectionSpec) {
	if !ShouldRewriteAppProbers(spec) {
		return
	}
	sidecar := FindSidecar(podSpec.Containers)
	if sidecar == nil {
		return
	}

	statusPort := extractStatusPort(sidecar)
	// Pilot agent statusPort is not defined, skip changing application http probe.
	if statusPort == -1 {
		return
	}
	if prober := DumpAppProbers(podSpec); prober != "" {
		// We don't have to escape json encoding here when using golang libraries.
		sidecar.Args = append(sidecar.Args,
			[]string{fmt.Sprintf("--%v", status.KubeAppProberCmdFlagName), prober}...)
	}
	// Now modify the container probers.
	for _, c := range podSpec.Containers {
		// Skip sidecar container.
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez := status.FormatProberURL(c.Name)
		if hg := convertAppProber(c.ReadinessProbe, readyz, statusPort); hg != nil {
			*c.ReadinessProbe.HTTPGet = *hg
		}
		if hg := convertAppProber(c.LivenessProbe, livez, statusPort); hg != nil {
			*c.LivenessProbe.HTTPGet = *hg
		}
	}
}

// createProbeRewritePatch generates the patch for webhook.
func createProbeRewritePatch(podSpec *corev1.PodSpec, spec *SidecarInjectionSpec) []rfc6902PatchOperation {
	if !ShouldRewriteAppProbers(spec) {
		return []rfc6902PatchOperation{}
	}
	patch := []rfc6902PatchOperation{}
	sidecar := FindSidecar(spec.Containers)
	if sidecar == nil {
		return nil
	}
	statusPort := extractStatusPort(sidecar)
	// Pilot agent statusPort is not defined, skip changing application http probe.
	if statusPort == -1 {
		return nil
	}
	for i, c := range podSpec.Containers {
		// Skip sidecar container.
		if c.Name == istioProxyContainerName {
			continue
		}
		portMap := map[string]int32{}
		for _, p := range c.Ports {
			portMap[p.Name] = p.ContainerPort
		}
		readyz, livez := status.FormatProberURL(c.Name)
		if after := convertAppProber(c.ReadinessProbe, readyz, statusPort); after != nil {
			patch = append(patch, rfc6902PatchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%v/readinessProbe/httpGet", i),
				Value: *after,
			})
		}
		if after := convertAppProber(c.LivenessProbe, livez, statusPort); after != nil {
			patch = append(patch, rfc6902PatchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%v/livenessProbe/httpGet", i),
				Value: *after,
			})
		}
	}
	return patch
}
