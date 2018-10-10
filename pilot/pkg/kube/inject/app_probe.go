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

// Package injects provides how we inject the Envoy sidecar.
package inject

import (
	"fmt"
  corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func appProbePath(kind string, containers []corev1.Container) string {
	for _, c := range containers {
		probe := c.ReadinessProbe
		if kind == "live" {
			probe = c.LivenessProbe
		}
		if probe == nil || probe.Handler.HTTPGet == nil {
			continue
		}
		// TODO(incfly): support more than one container probing.
		hp := probe.Handler.HTTPGet
		// TODO: handling named port? need to do a look up for the app container to find the mapping?
		// Any standard library for this?
		return fmt.Sprintf(":%v%v", hp.Port.IntVal, hp.Path)
	}
	return ""
}

func rewriteAppHTTPProbe(spec *SidecarInjectionSpec, podSpec *corev1.PodSpec) {
	// Remove the application's own http probe.
	resetFunc := func(probe *corev1.Probe, path string, statusPort int) {
		if probe == nil || probe.HTTPGet == nil {
			return
		}
		probe.HTTPGet.Path = path
		probe.HTTPGet.Port = intstr.FromInt(statusPort)
	}
	// TODO: figure out how to get the value of statusPort, either one of these:
	// - parsing sideCarSpecTmpl, find the argument.
	// - passing params from kubeinject.go of statusPort.
	// Second is better but requires more plumbing
	for _, c := range podSpec.Containers {
		resetFunc(c.ReadinessProbe, "/app/ready", 15020)
		resetFunc(c.LivenessProbe, "/app/live", 15020)
	}
}