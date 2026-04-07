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

package model

import (
	"net/url"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	networkwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	wasmextensions "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	extensions "istio.io/api/extensions/v1alpha1"
	typeapi "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/model/credentials"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	defaultRuntime = "envoy.wasm.runtime.v8"
	fileScheme     = "file"
	ociScheme      = "oci"

	WasmSecretEnv          = pm.WasmSecretEnv
	WasmPolicyEnv          = pm.WasmPolicyEnv
	WasmResourceVersionEnv = pm.WasmResourceVersionEnv

	// TrafficExtensionResourceNamePrefix is the prefix of the resource name of TrafficExtension,
	// preventing the name collision with other resources.
	TrafficExtensionResourceNamePrefix = "extensions.istio.io/trafficextension/"
)

func workloadModeForListenerClass(class istionetworking.ListenerClass) typeapi.WorkloadMode {
	switch class {
	case istionetworking.ListenerClassGateway:
		return typeapi.WorkloadMode_CLIENT
	case istionetworking.ListenerClassSidecarInbound:
		return typeapi.WorkloadMode_SERVER
	case istionetworking.ListenerClassSidecarOutbound:
		return typeapi.WorkloadMode_CLIENT
	case istionetworking.ListenerClassUndefined:
		// this should not happen, just in case
		return typeapi.WorkloadMode_CLIENT
	}
	return typeapi.WorkloadMode_CLIENT
}

type ListenerInfo struct {
	Port  int
	Class istionetworking.ListenerClass

	// Service that TrafficExtensions can attach to via targetRefs (optional)
	Services []*Service
}

func (listenerInfo ListenerInfo) WithService(service *Service) ListenerInfo {
	if service == nil {
		return listenerInfo
	}
	listenerInfo.Services = append(listenerInfo.Services, service)
	return listenerInfo
}

func matchTrafficSelectorCommon(ts *extensions.TrafficSelector, li ListenerInfo) bool {
	return matchMode(ts.GetMode(), li.Class) && matchPorts(ts.GetPorts(), li.Port)
}

func matchMode(workloadMode typeapi.WorkloadMode, class istionetworking.ListenerClass) bool {
	switch workloadMode {
	case typeapi.WorkloadMode_CLIENT_AND_SERVER, typeapi.WorkloadMode_UNDEFINED:
		return true
	default:
		return workloadMode == workloadModeForListenerClass(class)
	}
}

func matchPorts(portSelectors []*typeapi.PortSelector, port int) bool {
	if len(portSelectors) == 0 {
		// If there is no specified port, match with all the ports.
		return true
	}
	for _, ps := range portSelectors {
		if ps.GetNumber() != 0 && ps.GetNumber() == uint32(port) {
			return true
		}
	}
	return false
}

func buildDataSource(u *url.URL, urlString string, sha256 string) *core.AsyncDataSource {
	if u.Scheme == fileScheme {
		return &core.AsyncDataSource{
			Specifier: &core.AsyncDataSource_Local{
				Local: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: strings.TrimPrefix(urlString, "file://"),
					},
				},
			},
		}
	}

	return &core.AsyncDataSource{
		Specifier: &core.AsyncDataSource_Remote{
			Remote: &core.RemoteDataSource{
				HttpUri: &core.HttpUri{
					Uri:     u.String(),
					Timeout: durationpb.New(30 * time.Second), // TODO: make this configurable?
					HttpUpstreamType: &core.HttpUri_Cluster{
						// the agent will fetch this anyway, so no need for a cluster
						Cluster: "_",
					},
				},
				Sha256: sha256,
			},
		},
	}
}

// FilterChainType describes which Envoy filter chain type an extension applies to
type FilterChainType int

const (
	FilterChainTypeAny FilterChainType = iota
	FilterChainTypeHTTP
	FilterChainTypeNetwork
)

// filterChainTypeFromPluginType converts extensions.PluginType to FilterChainType
func filterChainTypeFromPluginType(pluginType extensions.PluginType) FilterChainType {
	switch pluginType {
	case extensions.PluginType_HTTP:
		return FilterChainTypeHTTP
	case extensions.PluginType_NETWORK:
		return FilterChainTypeNetwork
	case extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE:
		return FilterChainTypeHTTP // Return HTTP as default for backward compatibility.
	}
	return FilterChainTypeHTTP
}

// TrafficExtensionWrapper is a wrapper for extensions.TrafficExtension with additional runtime information
type TrafficExtensionWrapper struct {
	*extensions.TrafficExtension

	Name            string
	Namespace       string
	ResourceName    string
	ResourceVersion string
}

// GetTargetRef returns nil; TrafficExtension uses GetTargetRefs (plural) only.
// This satisfies the TargetablePolicy interface.
func (e *TrafficExtensionWrapper) GetTargetRef() *typeapi.PolicyTargetReference {
	return nil
}

func (e *TrafficExtensionWrapper) MatchListener(matcher WorkloadPolicyMatcher, li ListenerInfo) bool {
	if matcher.ShouldAttachPolicy(gvk.TrafficExtension, e.NamespacedName(), e) {
		return matchTrafficExtensionSelectors(e.Match, li)
	}
	return false
}

func (e *TrafficExtensionWrapper) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: e.Name, Namespace: e.Namespace}
}

func (e *TrafficExtensionWrapper) MatchType(chainType FilterChainType) bool {
	if e.GetLua() != nil {
		// Lua only supports HTTP filters
		return chainType == FilterChainTypeAny || chainType == FilterChainTypeHTTP
	}
	// For WASM, check the type field
	wasmType := filterChainTypeFromPluginType(e.GetWasm().GetType())
	return chainType == FilterChainTypeAny || chainType == wasmType
}

func (e *TrafficExtensionWrapper) BuildHTTPWasmFilter() *httpwasm.Wasm {
	if e.GetWasm() == nil {
		return nil
	}
	if !(e.GetWasm().Type == extensions.PluginType_HTTP || e.GetWasm().Type == extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE) {
		return nil
	}
	return &httpwasm.Wasm{
		Config: e.buildWasmConfig(),
	}
}

func (e *TrafficExtensionWrapper) BuildNetworkWasmFilter() *networkwasm.Wasm {
	if e.GetWasm() == nil {
		return nil
	}
	if e.GetWasm().Type != extensions.PluginType_NETWORK {
		return nil
	}
	return &networkwasm.Wasm{
		Config: e.buildWasmConfig(),
	}
}

func (e *TrafficExtensionWrapper) buildWasmConfig() *wasmextensions.PluginConfig {
	cfg := &anypb.Any{}
	if e.GetWasm().PluginConfig != nil && len(e.GetWasm().PluginConfig.Fields) > 0 {
		cfgJSON, err := protomarshal.ToJSON(e.GetWasm().PluginConfig)
		if err != nil {
			log.Warnf("trafficextension %v/%v discarded due to json marshaling error: %s", e.Namespace, e.Name, err)
			return nil
		}
		cfg = protoconv.MessageToAny(&wrapperspb.StringValue{
			Value: cfgJSON,
		})
	}

	u, err := url.Parse(e.GetWasm().Url)
	if err != nil {
		log.Warnf("trafficextension %v/%v discarded due to failure to parse URL: %s", e.Namespace, e.Name, err)
		return nil
	}
	// when no scheme is given, default to oci://
	if u.Scheme == "" {
		u.Scheme = ociScheme
	}

	datasource := buildDataSource(u, e.GetWasm().Url, e.GetWasm().Sha256)
	resourceName := e.Namespace + "." + e.Name

	wasmConfig := &wasmextensions.PluginConfig{
		Name:          resourceName,
		RootId:        e.GetWasm().PluginName,
		Configuration: cfg,
		Vm: buildVMConfig(datasource, e.ResourceVersion,
			e.GetWasm().ImagePullSecret, e.GetWasm().ImagePullPolicy, e.GetWasm().VmConfig),
	}

	switch e.GetWasm().FailStrategy {
	case extensions.FailStrategy_FAIL_OPEN:
		wasmConfig.FailurePolicy = wasmextensions.FailurePolicy_FAIL_OPEN
	case extensions.FailStrategy_FAIL_CLOSE:
		wasmConfig.FailurePolicy = wasmextensions.FailurePolicy_FAIL_CLOSED
	case extensions.FailStrategy_FAIL_RELOAD:
		wasmConfig.FailurePolicy = wasmextensions.FailurePolicy_FAIL_RELOAD
	}
	return wasmConfig
}

func matchTrafficExtensionSelectors(ts []*extensions.TrafficSelector, li ListenerInfo) bool {
	if (li.Class == istionetworking.ListenerClassUndefined && li.Port == 0) || len(ts) == 0 {
		return true
	}

	for _, match := range ts {
		if matchTrafficSelectorCommon(match, li) {
			return true
		}
	}
	return false
}

func convertToTrafficExtensionWrapper(originConfig config.Config) *TrafficExtensionWrapper {
	plugin := originConfig.DeepCopy()
	var trafficExt *extensions.TrafficExtension
	var ok bool
	if trafficExt, ok = plugin.Spec.(*extensions.TrafficExtension); !ok {
		return nil
	}

	wasm := trafficExt.GetWasm()
	lua := trafficExt.GetLua()

	if wasm != nil {
		// Validate WASM config
		if wasm.Url == "" {
			log.Warnf("trafficextension %v/%v discarded: wasm.url is required", plugin.Namespace, plugin.Name)
			return nil
		}
		u, err := url.Parse(wasm.Url)
		if err != nil {
			log.Warnf("trafficextension %v/%v discarded due to failure to parse wasm URL: %s", plugin.Namespace, plugin.Name, err)
			return nil
		}
		// when no scheme is given, default to oci://
		if u.Scheme == "" {
			u.Scheme = ociScheme
		}
		// Validate plugin config can be marshaled
		if wasm.PluginConfig != nil && len(wasm.PluginConfig.Fields) > 0 {
			_, err := protomarshal.ToJSON(wasm.PluginConfig)
			if err != nil {
				log.Warnf("trafficextension %v/%v discarded due to json marshaling error: %s", plugin.Namespace, plugin.Name, err)
				return nil
			}
		}
		// Normalize the image pull secret to the full resource name
		if wasm.ImagePullSecret != "" {
			// Convert user provided secret name to secret resource name.
			rn := credentials.ToResourceName(wasm.ImagePullSecret)
			// Parse the secret resource name.
			sr, err := credentials.ParseResourceName(rn, plugin.Namespace, "", "")
			if err != nil {
				log.Debugf("Failed to parse wasm secret resource name %v", err)
				wasm.ImagePullSecret = ""
			} else {
				// Forcely rewrite secret namespace to plugin namespace, since we require secret resource
				// referenced by TrafficExtension co-located with TrafficExtension in the same namespace.
				sr.Namespace = plugin.Namespace
				wasm.ImagePullSecret = sr.KubernetesResourceName()
			}
		}
	} else if lua != nil {
		// Validate Lua config
		if len(lua.InlineCode) == 0 {
			log.Warnf("trafficextension %v/%v discarded: lua.inlineCode cannot be empty", plugin.Namespace, plugin.Name)
			return nil
		}
		if len(lua.InlineCode) > 65536 {
			log.Warnf("trafficextension %v/%v discarded: lua.inlineCode exceeds maximum size of 64KB", plugin.Namespace, plugin.Name)
			return nil
		}
	} else {
		log.Warnf("trafficextension %v/%v discarded: neither wasm nor lua is set", plugin.Namespace, plugin.Name)
		return nil
	}

	return &TrafficExtensionWrapper{
		Name:             plugin.Name,
		Namespace:        plugin.Namespace,
		ResourceName:     TrafficExtensionResourceNamePrefix + plugin.Namespace + "." + plugin.Name,
		TrafficExtension: trafficExt,
		ResourceVersion:  plugin.ResourceVersion,
	}
}

func buildVMConfig(
	datasource *core.AsyncDataSource,
	resourceVersion string,
	imagePullSecret string,
	imagePullPolicy extensions.PullPolicy,
	vmConfig *extensions.VmConfig,
) *wasmextensions.PluginConfig_VmConfig {
	cfg := &wasmextensions.PluginConfig_VmConfig{
		VmConfig: &wasmextensions.VmConfig{
			Runtime: defaultRuntime,
			Code:    datasource,
			EnvironmentVariables: &wasmextensions.EnvironmentVariables{
				KeyValues: map[string]string{},
			},
		},
	}

	if imagePullSecret != "" {
		cfg.VmConfig.EnvironmentVariables.KeyValues[WasmSecretEnv] = imagePullSecret
	}

	if imagePullPolicy != extensions.PullPolicy_UNSPECIFIED_POLICY {
		cfg.VmConfig.EnvironmentVariables.KeyValues[WasmPolicyEnv] = imagePullPolicy.String()
	}

	cfg.VmConfig.EnvironmentVariables.KeyValues[WasmResourceVersionEnv] = resourceVersion

	if vmConfig != nil && len(vmConfig.Env) != 0 {
		hostEnvKeys := make([]string, 0, len(vmConfig.Env))
		for _, e := range vmConfig.Env {
			switch e.ValueFrom {
			case extensions.EnvValueSource_INLINE:
				cfg.VmConfig.EnvironmentVariables.KeyValues[e.Name] = e.Value
			case extensions.EnvValueSource_HOST:
				hostEnvKeys = append(hostEnvKeys, e.Name)
			}
		}
		cfg.VmConfig.EnvironmentVariables.HostEnvKeys = hostEnvKeys
	}

	return cfg
}
