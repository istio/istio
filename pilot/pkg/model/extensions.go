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
	structpb "google.golang.org/protobuf/types/known/structpb"
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

	// WasmPluginResourceNamePrefix is the prefix of the resource name of WasmPlugin,
	// preventing the name collision with other resources.
	WasmPluginResourceNamePrefix = "extensions.istio.io/wasmplugin/"

	// ExtensionFilterResourceNamePrefix is the prefix of the resource name of ExtensionFilter,
	// preventing the name collision with other resources.
	ExtensionFilterResourceNamePrefix = "extensions.istio.io/extensionfilter/"
)

// WasmPluginType defines the type of wasm plugin
type WasmPluginType int

const (
	WasmPluginTypeHTTP WasmPluginType = iota
	WasmPluginTypeNetwork
	WasmPluginTypeAny
)

func fromPluginType(pluginType extensions.PluginType) WasmPluginType {
	switch pluginType {
	case extensions.PluginType_HTTP:
		return WasmPluginTypeHTTP
	case extensions.PluginType_NETWORK:
		return WasmPluginTypeNetwork
	case extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE:
		return WasmPluginTypeHTTP // Return HTTP as default for backward compatibility.
	}
	return WasmPluginTypeHTTP
}

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

type WasmPluginWrapper struct {
	*extensions.WasmPlugin

	Name            string
	Namespace       string
	ResourceName    string
	ResourceVersion string
}

func (p *WasmPluginWrapper) GetPriority() *wrapperspb.Int32Value {
	return p.Priority
}

func (p *WasmPluginWrapper) MatchListener(matcher WorkloadPolicyMatcher, li WasmPluginListenerInfo) bool {
	if matcher.ShouldAttachPolicy(gvk.WasmPlugin, p.NamespacedName(), p) {
		return matchTrafficSelectors(p.Match, li)
	}

	// If it doesn't match one of the above cases, the plugin is not bound to this workload
	return false
}

func (p *WasmPluginWrapper) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: p.Name, Namespace: p.Namespace}
}

func (p *WasmPluginWrapper) MatchType(pluginType WasmPluginType) bool {
	return pluginType == WasmPluginTypeAny || pluginType == fromPluginType(p.WasmPlugin.Type)
}

func (p *WasmPluginWrapper) BuildHTTPWasmFilter() *httpwasm.Wasm {
	if !(p.Type == extensions.PluginType_HTTP || p.Type == extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE) {
		return nil
	}
	return &httpwasm.Wasm{
		Config: p.buildPluginConfig(),
	}
}

func (p *WasmPluginWrapper) BuildNetworkWasmFilter() *networkwasm.Wasm {
	if p.Type != extensions.PluginType_NETWORK {
		return nil
	}
	return &networkwasm.Wasm{
		Config: p.buildPluginConfig(),
	}
}

func (p *WasmPluginWrapper) buildPluginConfig() *wasmextensions.PluginConfig {
	return buildWasmPluginConfigCommon(
		p.Namespace, p.Name, p.ResourceVersion,
		p.WasmPlugin.Url, p.WasmPlugin.Sha256,
		p.WasmPlugin.PluginConfig, p.WasmPlugin.PluginName,
		p.WasmPlugin.FailStrategy,
		p.WasmPlugin.VmConfig, p.WasmPlugin.ImagePullSecret, p.WasmPlugin.ImagePullPolicy,
		"wasmplugin",
	)
}

type WasmPluginListenerInfo struct {
	Port  int
	Class istionetworking.ListenerClass

	// Service that WasmPlugins can attach to via targetRefs (optional)
	Services []*Service
}

func (listenerInfo WasmPluginListenerInfo) WithService(service *Service) WasmPluginListenerInfo {
	if service == nil {
		return listenerInfo
	}
	listenerInfo.Services = append(listenerInfo.Services, service)
	return listenerInfo
}

// If anyListener is used as a listener info,
// the listener is matched with any TrafficSelector.
var anyListener = WasmPluginListenerInfo{
	Port:  0,
	Class: istionetworking.ListenerClassUndefined,
}

// trafficSelector is an interface for matching traffic selectors.
// Both WasmPlugin_TrafficSelector and TrafficSelector implement this.
type trafficSelector interface {
	GetMode() typeapi.WorkloadMode
	GetPorts() []*typeapi.PortSelector
}

func matchTrafficSelectors(ts []*extensions.WasmPlugin_TrafficSelector, li WasmPluginListenerInfo) bool {
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

func matchTrafficSelectorCommon(ts trafficSelector, li WasmPluginListenerInfo) bool {
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

func convertToWasmPluginWrapper(originPlugin config.Config) *WasmPluginWrapper {
	var ok bool
	// Make a deep copy since we are going to mutate the resource later for secret env variable.
	// We do not want to mutate the underlying resource at informer cache.
	plugin := originPlugin.DeepCopy()
	var wasmPlugin *extensions.WasmPlugin
	if wasmPlugin, ok = plugin.Spec.(*extensions.WasmPlugin); !ok {
		return nil
	}

	if wasmPlugin.PluginConfig != nil && len(wasmPlugin.PluginConfig.Fields) > 0 {
		_, err := protomarshal.ToJSON(wasmPlugin.PluginConfig)
		if err != nil {
			log.Warnf("wasmplugin %v/%v discarded due to json marshaling error: %s", plugin.Namespace, plugin.Name, err)
			return nil
		}
	}

	u, err := url.Parse(wasmPlugin.Url)
	if err != nil {
		log.Warnf("wasmplugin %v/%v discarded due to failure to parse URL: %s", plugin.Namespace, plugin.Name, err)
		return nil
	}
	// when no scheme is given, default to oci://
	if u.Scheme == "" {
		u.Scheme = ociScheme
	}
	// Normalize the image pull secret to the full resource name.
	wasmPlugin.ImagePullSecret = toSecretResourceName(wasmPlugin.ImagePullSecret, plugin.Namespace)
	return &WasmPluginWrapper{
		Name:            plugin.Name,
		Namespace:       plugin.Namespace,
		ResourceName:    WasmPluginResourceNamePrefix + plugin.Namespace + "." + plugin.Name,
		WasmPlugin:      wasmPlugin,
		ResourceVersion: plugin.ResourceVersion,
	}
}

// toSecretResourceName converts a imagePullSecret to a resource name referenced at Wasm SDS.
// NOTE: the secret referenced by WasmPlugin has to be in the same namespace as the WasmPlugin,
// so this function makes sure that the secret resource name, which will be used to retrieve secret at
// xds generation time, has the same namespace as the WasmPlugin.
func toSecretResourceName(name, pluginNamespace string) string {
	if name == "" {
		return ""
	}
	// Convert user provided secret name to secret resource name.
	rn := credentials.ToResourceName(name)
	// Parse the secret resource name.
	sr, err := credentials.ParseResourceName(rn, pluginNamespace, "", "")
	if err != nil {
		log.Debugf("Failed to parse wasm secret resource name %v", err)
		return ""
	}
	// Forcely rewrite secret namespace to plugin namespace, since we require secret resource
	// referenced by WasmPlugin co-located with WasmPlugin in the same namespace.
	sr.Namespace = pluginNamespace
	return sr.KubernetesResourceName()
}

func buildDataSource(u *url.URL, wasmPlugin *extensions.WasmPlugin) *core.AsyncDataSource {
	if u.Scheme == fileScheme {
		return &core.AsyncDataSource{
			Specifier: &core.AsyncDataSource_Local{
				Local: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: strings.TrimPrefix(wasmPlugin.Url, "file://"),
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
				Sha256: wasmPlugin.Sha256,
			},
		},
	}
}

// buildWasmPluginConfigCommon builds a WASM plugin configuration from individual fields.
// This is shared between WasmPlugin and ExtensionFilter to avoid duplication.
func buildWasmPluginConfigCommon(
	namespace, name, resourceVersion string,
	wasmURL, sha256 string,
	pluginConfig *structpb.Struct,
	pluginName string,
	failStrategy extensions.FailStrategy,
	vmConfig *extensions.VmConfig,
	imagePullSecret string,
	imagePullPolicy extensions.PullPolicy,
	resourceType string, // "wasmplugin" or "extensionfilter" for logging
) *wasmextensions.PluginConfig {
	cfg := &anypb.Any{}
	if pluginConfig != nil && len(pluginConfig.Fields) > 0 {
		cfgJSON, err := protomarshal.ToJSON(pluginConfig)
		if err != nil {
			log.Warnf("%s %v/%v discarded due to json marshaling error: %s", resourceType, namespace, name, err)
			return nil
		}
		cfg = protoconv.MessageToAny(&wrapperspb.StringValue{
			Value: cfgJSON,
		})
	}

	u, err := url.Parse(wasmURL)
	if err != nil {
		log.Warnf("%s %v/%v discarded due to failure to parse URL: %s", resourceType, namespace, name, err)
		return nil
	}
	// when no scheme is given, default to oci://
	if u.Scheme == "" {
		u.Scheme = ociScheme
	}

	datasource := buildDataSource(u, &extensions.WasmPlugin{
		Url:    wasmURL,
		Sha256: sha256,
	})
	resourceName := namespace + "." + name

	wasmConfig := &wasmextensions.PluginConfig{
		Name:          resourceName,
		RootId:        pluginName,
		Configuration: cfg,
		Vm:            buildVMConfig(datasource, resourceVersion, &extensions.WasmPlugin{
			VmConfig:        vmConfig,
			ImagePullSecret: imagePullSecret,
			ImagePullPolicy: imagePullPolicy,
		}),
	}

	switch failStrategy {
	case extensions.FailStrategy_FAIL_OPEN:
		wasmConfig.FailurePolicy = wasmextensions.FailurePolicy_FAIL_OPEN
	case extensions.FailStrategy_FAIL_CLOSE:
		wasmConfig.FailurePolicy = wasmextensions.FailurePolicy_FAIL_CLOSED
	case extensions.FailStrategy_FAIL_RELOAD:
		wasmConfig.FailurePolicy = wasmextensions.FailurePolicy_FAIL_RELOAD
	}
	return wasmConfig
}

// FilterType defines whether an ExtensionFilter is Lua or WASM based
type FilterType int

const (
	FilterTypeWasm FilterType = iota
	FilterTypeLua
)

// FilterChainType describes which Envoy filter chain type an extension applies to
type FilterChainType int

const (
	FilterChainTypeAny FilterChainType = iota
	FilterChainTypeHTTP
	FilterChainTypeNetwork
)

// ToWasmPluginType converts FilterChainType to WasmPluginType for compatibility with existing code
func (f FilterChainType) ToWasmPluginType() WasmPluginType {
	switch f {
	case FilterChainTypeHTTP:
		return WasmPluginTypeHTTP
	case FilterChainTypeNetwork:
		return WasmPluginTypeNetwork
	default:
		return WasmPluginTypeAny
	}
}

// FilterChainTypeFromWasmPluginType converts WasmPluginType to FilterChainType
func FilterChainTypeFromWasmPluginType(w WasmPluginType) FilterChainType {
	switch w {
	case WasmPluginTypeHTTP:
		return FilterChainTypeHTTP
	case WasmPluginTypeNetwork:
		return FilterChainTypeNetwork
	default:
		return FilterChainTypeAny
	}
}

// ExtensionFilterWrapper is a wrapper for extensions.ExtensionFilter with additional runtime information
type ExtensionFilterWrapper struct {
	*extensions.ExtensionFilter

	Name            string
	Namespace       string
	ResourceName    string
	ResourceVersion string
	FilterType      FilterType
}

func (e *ExtensionFilterWrapper) GetPriority() *wrapperspb.Int32Value {
	return e.Priority
}

func (e *ExtensionFilterWrapper) MatchListener(matcher WorkloadPolicyMatcher, li WasmPluginListenerInfo) bool {
	if matcher.ShouldAttachPolicy(gvk.ExtensionFilter, e.NamespacedName(), e) {
		return matchExtensionFilterTrafficSelectors(e.Match, li)
	}
	return false
}

func (e *ExtensionFilterWrapper) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: e.Name, Namespace: e.Namespace}
}

func (e *ExtensionFilterWrapper) MatchType(chainType FilterChainType) bool {
	if e.FilterType == FilterTypeLua {
		// Lua only supports HTTP filters
		return chainType == FilterChainTypeAny || chainType == FilterChainTypeHTTP
	}
	// For WASM, check the type field
	wasmType := FilterChainTypeFromWasmPluginType(fromPluginType(e.Wasm.Type))
	return chainType == FilterChainTypeAny || chainType == wasmType
}

func (e *ExtensionFilterWrapper) BuildHTTPWasmFilter() *httpwasm.Wasm {
	if e.FilterType != FilterTypeWasm || e.Wasm == nil {
		return nil
	}
	if !(e.Wasm.Type == extensions.PluginType_HTTP || e.Wasm.Type == extensions.PluginType_UNSPECIFIED_PLUGIN_TYPE) {
		return nil
	}
	return &httpwasm.Wasm{
		Config: e.buildWasmPluginConfig(),
	}
}

func (e *ExtensionFilterWrapper) BuildNetworkWasmFilter() *networkwasm.Wasm {
	if e.FilterType != FilterTypeWasm || e.Wasm == nil {
		return nil
	}
	if e.Wasm.Type != extensions.PluginType_NETWORK {
		return nil
	}
	return &networkwasm.Wasm{
		Config: e.buildWasmPluginConfig(),
	}
}

func (e *ExtensionFilterWrapper) buildWasmPluginConfig() *wasmextensions.PluginConfig {
	return buildWasmPluginConfigCommon(
		e.Namespace, e.Name, e.ResourceVersion,
		e.Wasm.Url, e.Wasm.Sha256,
		e.Wasm.PluginConfig, e.Wasm.PluginName,
		e.Wasm.FailStrategy,
		e.Wasm.VmConfig, e.Wasm.ImagePullSecret, e.Wasm.ImagePullPolicy,
		"extensionfilter",
	)
}

func matchExtensionFilterTrafficSelectors(ts []*extensions.TrafficSelector, li WasmPluginListenerInfo) bool {
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

func convertToExtensionFilterWrapper(originConfig config.Config) *ExtensionFilterWrapper {
	plugin := originConfig.DeepCopy()
	var extFilter *extensions.ExtensionFilter
	var ok bool
	if extFilter, ok = plugin.Spec.(*extensions.ExtensionFilter); !ok {
		return nil
	}

	// Determine filter type - must have exactly one of wasm or lua
	var filterType FilterType
	if extFilter.Wasm != nil && extFilter.Lua != nil {
		log.Warnf("extensionfilter %v/%v discarded: both wasm and lua are set (must have exactly one)", plugin.Namespace, plugin.Name)
		return nil
	}

	if extFilter.Wasm != nil {
		filterType = FilterTypeWasm
		// Validate WASM config
		if extFilter.Wasm.Url == "" {
			log.Warnf("extensionfilter %v/%v discarded: wasm.url is required", plugin.Namespace, plugin.Name)
			return nil
		}
		u, err := url.Parse(extFilter.Wasm.Url)
		if err != nil {
			log.Warnf("extensionfilter %v/%v discarded due to failure to parse wasm URL: %s", plugin.Namespace, plugin.Name, err)
			return nil
		}
		// when no scheme is given, default to oci://
		if u.Scheme == "" {
			u.Scheme = ociScheme
		}
		// Validate plugin config can be marshaled
		if extFilter.Wasm.PluginConfig != nil && len(extFilter.Wasm.PluginConfig.Fields) > 0 {
			_, err := protomarshal.ToJSON(extFilter.Wasm.PluginConfig)
			if err != nil {
				log.Warnf("extensionfilter %v/%v discarded due to json marshaling error: %s", plugin.Namespace, plugin.Name, err)
				return nil
			}
		}
		// Normalize the image pull secret to the full resource name
		extFilter.Wasm.ImagePullSecret = toSecretResourceName(extFilter.Wasm.ImagePullSecret, plugin.Namespace)
	} else if extFilter.Lua != nil {
		filterType = FilterTypeLua
		// Validate Lua config
		if len(extFilter.Lua.InlineCode) == 0 {
			log.Warnf("extensionfilter %v/%v discarded: lua.inlineCode cannot be empty", plugin.Namespace, plugin.Name)
			return nil
		}
		if len(extFilter.Lua.InlineCode) > 65536 {
			log.Warnf("extensionfilter %v/%v discarded: lua.inlineCode exceeds maximum size of 64KB", plugin.Namespace, plugin.Name)
			return nil
		}
	} else {
		log.Warnf("extensionfilter %v/%v discarded: neither wasm nor lua is set", plugin.Namespace, plugin.Name)
		return nil
	}

	return &ExtensionFilterWrapper{
		Name:            plugin.Name,
		Namespace:       plugin.Namespace,
		ResourceName:    ExtensionFilterResourceNamePrefix + plugin.Namespace + "." + plugin.Name,
		ExtensionFilter: extFilter,
		ResourceVersion: plugin.ResourceVersion,
		FilterType:      filterType,
	}
}

func buildVMConfig(
	datasource *core.AsyncDataSource,
	resourceVersion string,
	wasmPlugin *extensions.WasmPlugin,
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

	if wasmPlugin.ImagePullSecret != "" {
		cfg.VmConfig.EnvironmentVariables.KeyValues[WasmSecretEnv] = wasmPlugin.ImagePullSecret
	}

	if wasmPlugin.ImagePullPolicy != extensions.PullPolicy_UNSPECIFIED_POLICY {
		cfg.VmConfig.EnvironmentVariables.KeyValues[WasmPolicyEnv] = wasmPlugin.ImagePullPolicy.String()
	}

	cfg.VmConfig.EnvironmentVariables.KeyValues[WasmResourceVersionEnv] = resourceVersion

	vm := wasmPlugin.VmConfig
	if vm != nil && len(vm.Env) != 0 {
		hostEnvKeys := make([]string, 0, len(vm.Env))
		for _, e := range vm.Env {
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
