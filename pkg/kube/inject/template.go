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

package inject

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/log"
)

var InjectionFuncmap = createInjectionFuncmap()

func createInjectionFuncmap() template.FuncMap {
	return template.FuncMap{
		"formatDuration":      formatDuration,
		"isset":               isset,
		"excludeInboundPort":  excludeInboundPort,
		"includeInboundPorts": includeInboundPorts,
		"kubevirtInterfaces":  kubevirtInterfaces,
		"applicationPorts":    applicationPorts,
		"annotation":          getAnnotation,
		"valueOrDefault":      valueOrDefault,
		"toJSON":              toJSON,
		"toJson":              toJSON, // Used by, e.g. Istio 1.0.5 template sidecar-injector-configmap.yaml
		"fromJSON":            fromJSON,
		"structToJSON":        structToJSON,
		"protoToJSON":         protoToJSON,
		"toYaml":              toYaml,
		"indent":              indent,
		"directory":           directory,
		"contains":            flippedContains,
		"toLower":             strings.ToLower,
		"appendMultusNetwork": appendMultusNetwork,
		"env":                 env,
	}
}

// Allows the template to use env variables from istiod.
// Istiod will use a custom template, without 'values.yaml', and the pod will have
// an optional 'vendor' configmap where additional settings can be defined.
func env(key string, def string) string {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	return val
}

func formatDuration(in *durationpb.Duration) string {
	return in.AsDuration().String()
}

func isset(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}

func directory(filepath string) string {
	dir, _ := path.Split(filepath)
	return dir
}

func flippedContains(needle, haystack string) bool {
	return strings.Contains(haystack, needle)
}

func excludeInboundPort(port interface{}, excludedInboundPorts string) string {
	portStr := strings.TrimSpace(fmt.Sprint(port))
	if len(portStr) == 0 || portStr == "0" {
		// Nothing to do.
		return excludedInboundPorts
	}

	// Exclude the readiness port if not already excluded.
	ports := splitPorts(excludedInboundPorts)
	outPorts := make([]string, 0, len(ports))
	for _, port := range ports {
		if port == portStr {
			// The port is already excluded.
			return excludedInboundPorts
		}
		port = strings.TrimSpace(port)
		if len(port) > 0 {
			outPorts = append(outPorts, port)
		}
	}

	// The port was not already excluded - exclude it now.
	outPorts = append(outPorts, portStr)
	return strings.Join(outPorts, ",")
}

func valueOrDefault(value interface{}, defaultValue interface{}) interface{} {
	if value == "" || value == nil {
		return defaultValue
	}
	return value
}

func toJSON(m map[string]string) string {
	if m == nil {
		return "{}"
	}

	ba, err := json.Marshal(m)
	if err != nil {
		log.Warnf("Unable to marshal %v", m)
		return "{}"
	}

	return string(ba)
}

func fromJSON(j string) interface{} {
	var m interface{}
	err := json.Unmarshal([]byte(j), &m)
	if err != nil {
		log.Warnf("Unable to unmarshal %s", j)
		return "{}"
	}

	return m
}

func indent(spaces int, source string) string {
	res := strings.Split(source, "\n")
	for i, line := range res {
		if i > 0 {
			res[i] = fmt.Sprintf(fmt.Sprintf("%% %ds%%s", spaces), "", line)
		}
	}
	return strings.Join(res, "\n")
}

func toYaml(value interface{}) string {
	y, err := yaml.Marshal(value)
	if err != nil {
		log.Warnf("Unable to marshal %v", value)
		return ""
	}

	return string(y)
}

func getAnnotation(meta metav1.ObjectMeta, name string, defaultValue interface{}) string {
	value, ok := meta.Annotations[name]
	if !ok {
		value = fmt.Sprint(defaultValue)
	}
	return value
}

func appendMultusNetwork(existingValue, istioCniNetwork string) string {
	if existingValue == "" {
		return istioCniNetwork
	}
	i := strings.LastIndex(existingValue, "]")
	isJSON := i != -1
	if isJSON {
		networks := []map[string]interface{}{}
		err := json.Unmarshal([]byte(existingValue), &networks)
		if err != nil {
			// existingValue is not valid JSON; nothing we can do but skip injection
			log.Warnf("Unable to unmarshal Multus Network annotation JSON value: %v", err)
			return existingValue
		}
		for _, net := range networks {
			if net["name"] == istioCniNetwork {
				return existingValue
			}
		}
		return existingValue[0:i] + fmt.Sprintf(`, {"name": "%s"}`, istioCniNetwork) + existingValue[i:]
	}
	for _, net := range strings.Split(existingValue, ",") {
		if strings.TrimSpace(net) == istioCniNetwork {
			return existingValue
		}
	}
	return existingValue + ", " + istioCniNetwork
}

// this function is no longer used by the template but kept around for backwards compatibility
func applicationPorts(containers []corev1.Container) string {
	return getContainerPorts(containers, func(c corev1.Container) bool {
		return c.Name != ProxyContainerName
	})
}

func includeInboundPorts(containers []corev1.Container) string {
	// Include the ports from all containers in the deployment.
	return getContainerPorts(containers, func(corev1.Container) bool { return true })
}

func getPortsForContainer(container corev1.Container) []string {
	parts := make([]string, 0)
	for _, p := range container.Ports {
		if p.Protocol == corev1.ProtocolUDP || p.Protocol == corev1.ProtocolSCTP {
			continue
		}
		parts = append(parts, strconv.Itoa(int(p.ContainerPort)))
	}
	return parts
}

func getContainerPorts(containers []corev1.Container, shouldIncludePorts func(corev1.Container) bool) string {
	parts := make([]string, 0)
	for _, c := range containers {
		if shouldIncludePorts(c) {
			parts = append(parts, getPortsForContainer(c)...)
		}
	}

	return strings.Join(parts, ",")
}

func kubevirtInterfaces(s string) string {
	return s
}

func structToJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}

	ba, err := json.Marshal(v)
	if err != nil {
		log.Warnf("Unable to marshal %v", v)
		return "{}"
	}

	return string(ba)
}

func protoToJSON(v proto.Message) string {
	v = cleanProxyConfig(v)
	if v == nil {
		return "{}"
	}

	ba, err := protomarshal.ToJSON(v)
	if err != nil {
		log.Warnf("Unable to marshal %v: %v", v, err)
		return "{}"
	}

	return ba
}

// Rather than dump the entire proxy config, we remove fields that are default
// This makes the pod spec much smaller
// This is not comprehensive code, but nothing will break if this misses some fields
func cleanProxyConfig(msg proto.Message) proto.Message {
	originalProxyConfig, ok := msg.(*meshconfig.ProxyConfig)
	if !ok || originalProxyConfig == nil {
		return msg
	}
	pc := proto.Clone(originalProxyConfig).(*meshconfig.ProxyConfig)
	defaults := mesh.DefaultProxyConfig()
	if pc.ConfigPath == defaults.ConfigPath {
		pc.ConfigPath = ""
	}
	if pc.BinaryPath == defaults.BinaryPath {
		pc.BinaryPath = ""
	}
	if pc.ControlPlaneAuthPolicy == defaults.ControlPlaneAuthPolicy {
		pc.ControlPlaneAuthPolicy = 0
	}
	if x, ok := pc.GetClusterName().(*meshconfig.ProxyConfig_ServiceCluster); ok {
		if x.ServiceCluster == defaults.GetClusterName().(*meshconfig.ProxyConfig_ServiceCluster).ServiceCluster {
			pc.ClusterName = nil
		}
	}

	if proto.Equal(pc.DrainDuration, defaults.DrainDuration) {
		pc.DrainDuration = nil
	}
	if proto.Equal(pc.TerminationDrainDuration, defaults.TerminationDrainDuration) {
		pc.TerminationDrainDuration = nil
	}
	if proto.Equal(pc.ParentShutdownDuration, defaults.ParentShutdownDuration) {
		pc.ParentShutdownDuration = nil
	}
	if pc.DiscoveryAddress == defaults.DiscoveryAddress {
		pc.DiscoveryAddress = ""
	}
	if proto.Equal(pc.EnvoyMetricsService, defaults.EnvoyMetricsService) {
		pc.EnvoyMetricsService = nil
	}
	if proto.Equal(pc.EnvoyAccessLogService, defaults.EnvoyAccessLogService) {
		pc.EnvoyAccessLogService = nil
	}
	if proto.Equal(pc.Tracing, defaults.Tracing) {
		pc.Tracing = nil
	}
	if pc.ProxyAdminPort == defaults.ProxyAdminPort {
		pc.ProxyAdminPort = 0
	}
	if pc.StatNameLength == defaults.StatNameLength {
		pc.StatNameLength = 0
	}
	if pc.StatusPort == defaults.StatusPort {
		pc.StatusPort = 0
	}
	if proto.Equal(pc.Concurrency, defaults.Concurrency) {
		pc.Concurrency = nil
	}
	if len(pc.ProxyMetadata) == 0 {
		pc.ProxyMetadata = nil
	}
	return proto.Message(pc)
}
