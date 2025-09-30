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

package wasm

import (
	"fmt"
	"strings"
	"sync"
	"time"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	httprbac "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	networkrbac "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	networkwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	wasmextensions "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"github.com/hashicorp/go-multierror"
	anypb "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/util/istiomultierror"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	allowHTTPTypedConfig, _ = anypb.New(&httprbac.RBAC{
		// no rules mean allow all.
		RulesStatPrefix: "wasm-default-allow",
	})

	denyHTTPTypedConfig, _ = anypb.New(&httprbac.RBAC{
		// empty rule means deny all.
		Rules:           &rbacv3.RBAC{},
		RulesStatPrefix: "wasm-default-deny",
	})

	allowNetworkTypeConfig, _ = anypb.New(&networkrbac.RBAC{
		// no rules mean allow all.
		StatPrefix: "wasm-default-allow",
	})
	denyNetworkTypedConfig, _ = anypb.New(&networkrbac.RBAC{
		// empty rule means deny all.
		Rules:      &rbacv3.RBAC{},
		StatPrefix: "wasm-default-deny",
	})
)

func createHTTPDefaultFilter(name string, failOpen bool) (*anypb.Any, error) {
	var tc *anypb.Any
	if failOpen {
		tc = allowHTTPTypedConfig
	} else {
		tc = denyHTTPTypedConfig
	}
	ec := &core.TypedExtensionConfig{
		Name:        name,
		TypedConfig: tc,
	}
	return anypb.New(ec)
}

func createNetworkDefaultFilter(name string, failOpen bool) (*anypb.Any, error) {
	var tc *anypb.Any
	if failOpen {
		tc = allowNetworkTypeConfig
	} else {
		tc = denyNetworkTypedConfig
	}
	ec := &core.TypedExtensionConfig{
		Name:        name,
		TypedConfig: tc,
	}
	return anypb.New(ec)
}

// MaybeConvertWasmExtensionConfig converts any presence of module remote download to local file.
// It downloads the Wasm module and stores the module locally in the file system.
func MaybeConvertWasmExtensionConfig(resources []*anypb.Any, cache Cache) error {
	var wg sync.WaitGroup

	numResources := len(resources)
	convertErrs := make([]error, numResources)
	wg.Add(numResources)

	startTime := time.Now()
	defer func() {
		wasmConfigConversionDuration.Record(float64(time.Since(startTime).Milliseconds()))
	}()

	for i := 0; i < numResources; i++ {
		go func(i int) {
			defer wg.Done()
			extConfig, wasmHTTPConfig, wasmNetworkConfig, err := tryUnmarshal(resources[i])
			if err != nil {
				wasmConfigConversionCount.
					With(resultTag.Value(unmarshalFailure)).
					Increment()
				convertErrs[i] = err
				return
			}

			if extConfig == nil || (wasmHTTPConfig == nil && wasmNetworkConfig == nil) {
				// If there is no config, it is not wasm config.
				// Let's bypass the ECDS resource.
				wasmConfigConversionCount.
					With(resultTag.Value(noRemoteLoad)).
					Increment()
				return
			}
			if wasmHTTPConfig != nil {
				newExtensionConfig, err := convertHTTPWasmConfigFromRemoteToLocal(extConfig, wasmHTTPConfig, cache)
				if err != nil {
					failOpen := httpWasmFailOpen(wasmHTTPConfig)
					rbacFilter := "deny"
					if failOpen {
						rbacFilter = "allow"
					}
					wasmLog.Errorf("error in converting the wasm config to local: %v. applying %s RBAC filter", err, rbacFilter)
					// Use NOOP filter because the download failed.
					// nolint: staticcheck // FailOpen deprecated
					newExtensionConfig, err = createHTTPDefaultFilter(extConfig.GetName(), failOpen)
					if err != nil {
						// If the fallback is failing, send the Nack regardless of fail_open.
						err = fmt.Errorf("failed to create allow-all filter as a fallback of %s Wasm Module: %w", extConfig.GetName(), err)
						convertErrs[i] = err
						return
					}
				}
				resources[i] = newExtensionConfig
			} else {
				newExtensionConfig, err := convertNetworkWasmConfigFromRemoteToLocal(extConfig, wasmNetworkConfig, cache)
				if err != nil {
					failOpen := networkWasmFailOpen(wasmNetworkConfig)
					rbacFilter := "deny"
					if failOpen {
						rbacFilter = "allow"
					}
					wasmLog.Errorf("error in converting the wasm config to local: %v. applying %s RBAC filter", err, rbacFilter)

					// Use NOOP filter because the download failed.
					newExtensionConfig, err = createNetworkDefaultFilter(extConfig.GetName(), failOpen)
					if err != nil {
						// If the fallback is failing, send the Nack regardless of fail_open.
						err = fmt.Errorf("failed to create allow-all filter as a fallback of %s Wasm Module: %w", extConfig.GetName(), err)
						convertErrs[i] = err
						return
					}
				}
				resources[i] = newExtensionConfig
			}
		}(i)
	}

	wg.Wait()
	err := multierror.Append(istiomultierror.New(), convertErrs...).ErrorOrNil()
	if err != nil {
		wasmLog.Errorf("error in applying failopen rbac config: %v", err)
	}
	return err
}

func httpWasmFailOpen(http *httpwasm.Wasm) bool {
	if failurePolicy := http.GetConfig().FailurePolicy; failurePolicy != wasmextensions.FailurePolicy_UNSPECIFIED {
		return failurePolicy == wasmextensions.FailurePolicy_FAIL_OPEN
	}

	// FailOpen deprecated
	return http.GetConfig().GetFailOpen() // nolint: staticcheck
}

func networkWasmFailOpen(network *networkwasm.Wasm) bool {
	if failurePolicy := network.GetConfig().FailurePolicy; failurePolicy != wasmextensions.FailurePolicy_UNSPECIFIED {
		return failurePolicy == wasmextensions.FailurePolicy_FAIL_OPEN
	}

	// FailOpen deprecated
	return network.GetConfig().GetFailOpen() // nolint: staticcheck
}

// tryUnmarshal returns the typed extension config and wasm config by unmarsharling `resource`,
// if `resource` is a wasm config loading a wasm module from the remote site.
// It returns `nil` for both the typed extension config and wasm config if it is not for the remote wasm or has an error.
func tryUnmarshal(resource *anypb.Any) (*core.TypedExtensionConfig, *httpwasm.Wasm, *networkwasm.Wasm, error) {
	ec := &core.TypedExtensionConfig{}
	wasmHTTPFilterConfig := &httpwasm.Wasm{}
	wasmNetworkFilterConfig := &networkwasm.Wasm{}
	wasmNetwork := false
	if err := resource.UnmarshalTo(ec); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to unmarshal extension config resource: %w", err)
	}

	// Wasm filter can be configured using typed struct and Wasm filter type
	switch {
	case ec.GetTypedConfig() == nil:
		return nil, nil, nil, fmt.Errorf("typed extension config %+v does not contain any typed config", ec)
	case ec.GetTypedConfig().TypeUrl == model.WasmHTTPFilterType:
		if err := ec.GetTypedConfig().UnmarshalTo(wasmHTTPFilterConfig); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to unmarshal extension config resource into Wasm HTTP filter: %w", err)
		}
	case ec.GetTypedConfig().TypeUrl == model.WasmNetworkFilterType:
		wasmNetwork = true
		if err := ec.GetTypedConfig().UnmarshalTo(wasmNetworkFilterConfig); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to unmarshal extension config resource into Wasm Network filter: %w", err)
		}
	case ec.GetTypedConfig().TypeUrl == model.TypedStructType:
		typedStruct := &udpa.TypedStruct{}
		wasmTypedConfig := ec.GetTypedConfig()
		if err := wasmTypedConfig.UnmarshalTo(typedStruct); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to unmarshal typed config for wasm filter: %w", err)
		}

		if typedStruct.TypeUrl == model.WasmHTTPFilterType {
			if err := protomarshal.StructToMessageSlow(typedStruct.Value, wasmHTTPFilterConfig); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to convert extension config struct %+v to Wasm Network filter", typedStruct)
			}
		} else if typedStruct.TypeUrl == model.WasmNetworkFilterType {
			wasmNetwork = true
			if err := protomarshal.StructToMessageSlow(typedStruct.Value, wasmNetworkFilterConfig); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to convert extension config struct %+v to Wasm HTTP filter", typedStruct)
			}
		} else {
			// This is not a Wasm filter.
			wasmLog.Debugf("typed extension config %+v does not contain wasm http filter", typedStruct)
			return nil, nil, nil, nil
		}
	default:
		// This is not a Wasm filter.
		wasmLog.Debugf("cannot find typed config or typed struct in %+v", ec)
		return nil, nil, nil, nil
	}

	// At this point, we should have wasmNetworkFilterConfig or wasmHTTPFilterConfig should be unmarshalled.
	if wasmNetwork {
		if wasmNetworkFilterConfig.Config.GetVmConfig().GetCode().GetRemote() == nil {
			if wasmNetworkFilterConfig.Config.GetVmConfig().GetCode().GetLocal() == nil {
				return nil, nil, nil, fmt.Errorf("no remote and local load found in Wasm Network filter %+v", wasmNetworkFilterConfig)
			}
			// This has a local Wasm. Let's bypass it.
			wasmLog.Debugf("no remote load found in Wasm Network filter %+v", wasmNetworkFilterConfig)
			return nil, nil, nil, nil
		}
		return ec, nil, wasmNetworkFilterConfig, nil
	}
	if wasmHTTPFilterConfig.Config.GetVmConfig().GetCode().GetRemote() == nil {
		if wasmHTTPFilterConfig.Config.GetVmConfig().GetCode().GetLocal() == nil {
			return nil, nil, nil, fmt.Errorf("no remote and local load found in Wasm HTTP filter %+v", wasmHTTPFilterConfig)
		}
		// This has a local Wasm. Let's bypass it.
		wasmLog.Debugf("no remote load found in Wasm HTTP filter %+v", wasmHTTPFilterConfig)
		return nil, nil, nil, nil
	}

	return ec, wasmHTTPFilterConfig, nil, nil
}

func convertHTTPWasmConfigFromRemoteToLocal(ec *core.TypedExtensionConfig, wasmHTTPFilterConfig *httpwasm.Wasm, cache Cache) (*anypb.Any, error) {
	status := conversionSuccess
	defer func() {
		wasmConfigConversionCount.
			With(resultTag.Value(status)).
			Increment()
	}()

	// ec.Name is resourceName.
	// https://github.com/istio/istio/blob/9ea7ad532a9cc58a3564143d41ac89a61aaa8058/pilot/pkg/networking/core/v1alpha3/extension/wasmplugin.go#L103
	err := rewriteVMConfig(ec.Name, wasmHTTPFilterConfig.Config.GetVmConfig(), &status, cache, wasmHTTPFilterConfig.Config.Name)
	if err != nil {
		return nil, err
	}

	wasmTypedConfig, err := anypb.New(wasmHTTPFilterConfig)
	if err != nil {
		status = marshalFailure
		return nil, fmt.Errorf("failed to marshal new wasm HTTP filter %+v to protobuf Any: %w", wasmHTTPFilterConfig, err)
	}
	ec.TypedConfig = wasmTypedConfig
	wasmLog.Debugf("new extension config resource %+v", ec)

	nec, err := anypb.New(ec)
	if err != nil {
		status = marshalFailure
		return nil, fmt.Errorf("failed to marshal new extension config resource: %w", err)
	}

	// At this point, we are certain that wasm module has been downloaded and config is rewritten.
	// ECDS will be rewritten successfully.
	return nec, nil
}

func convertNetworkWasmConfigFromRemoteToLocal(ec *core.TypedExtensionConfig, wasmNetworkFilterConfig *networkwasm.Wasm, cache Cache) (*anypb.Any, error) {
	status := conversionSuccess
	defer func() {
		wasmConfigConversionCount.
			With(resultTag.Value(status)).
			Increment()
	}()

	// ec.Name is resourceName.
	// https://github.com/istio/istio/blob/9ea7ad532a9cc58a3564143d41ac89a61aaa8058/pilot/pkg/networking/core/v1alpha3/extension/wasmplugin.go#L103
	err := rewriteVMConfig(ec.Name, wasmNetworkFilterConfig.Config.GetVmConfig(), &status, cache, wasmNetworkFilterConfig.Config.Name)
	if err != nil {
		return nil, err
	}
	wasmTypedConfig, err := anypb.New(wasmNetworkFilterConfig)
	if err != nil {
		status = marshalFailure
		return nil, fmt.Errorf("failed to marshal new wasm Network filter %+v to protobuf Any: %w", wasmNetworkFilterConfig, err)
	}
	ec.TypedConfig = wasmTypedConfig
	wasmLog.Debugf("new extension config resource %+v", ec)

	nec, err := anypb.New(ec)
	if err != nil {
		status = marshalFailure
		return nil, fmt.Errorf("failed to marshal new extension config resource: %w", err)
	}

	// At this point, we are certain that wasm module has been downloaded and config is rewritten.
	// ECDS will be rewritten successfully.
	return nec, nil
}

func rewriteVMConfig(resourceName string, vm *wasmextensions.VmConfig, status *string, cache Cache, configName string) error {
	envs := vm.GetEnvironmentVariables()
	var pullSecret []byte
	pullPolicy := Unspecified
	resourceVersion := ""
	if envs != nil {
		if sec, found := envs.KeyValues[model.WasmSecretEnv]; found {
			if sec == "" {
				*status = fetchFailure
				return fmt.Errorf("cannot fetch Wasm module %v: missing image pulling secret", configName)
			}
			pullSecret = []byte(sec)
		}

		if ps, found := envs.KeyValues[model.WasmPolicyEnv]; found {
			if p, found := PullPolicyValues[ps]; found {
				pullPolicy = p
			}
		}
		resourceVersion = envs.KeyValues[model.WasmResourceVersionEnv]

		// Strip all internal env variables(with ISTIO_META) from VM env variable.
		// These env variables are added by Istio control plane and meant to be consumed by the
		// agent for image pulling control should not be leaked to Envoy or the Wasm extension runtime.
		for k := range envs.KeyValues {
			if strings.HasPrefix(k, bootstrap.IstioMetaPrefix) {
				delete(envs.KeyValues, k)
			}
		}
		if len(envs.KeyValues) == 0 {
			if len(envs.HostEnvKeys) == 0 {
				vm.EnvironmentVariables = nil
			} else {
				envs.KeyValues = nil
			}
		}
	}
	remote := vm.GetCode().GetRemote()
	httpURI := remote.GetHttpUri()
	if httpURI == nil {
		*status = missRemoteFetchHint
		return fmt.Errorf("wasm remote fetch %+v does not have httpUri specified for config %s", remote, configName)
	}
	// checksum sent by istiod can be "nil" if not set by user - magic value used to avoid unmarshaling errors
	if remote.Sha256 == "nil" {
		remote.Sha256 = ""
	}

	// Default timeout, without this, if a user does not specify a timeout in the config, it fails with deadline exceeded
	// while building transport in go container.
	timeout := time.Second * 5
	if remote.GetHttpUri().Timeout != nil {
		// This is always 30s, because the timeout is set by the control plane when converted to WasmPluginWrapper.
		// see buildDataSource() in pilot/pkg/model/extensions.go
		timeout = remote.GetHttpUri().Timeout.AsDuration()
	}
	f, err := cache.Get(httpURI.GetUri(), GetOptions{
		Checksum:        remote.Sha256,
		ResourceName:    resourceName,
		ResourceVersion: resourceVersion,
		RequestTimeout:  timeout,
		PullSecret:      pullSecret,
		PullPolicy:      pullPolicy,
	})
	if err != nil {
		*status = fetchFailure
		return fmt.Errorf("cannot fetch Wasm module %v: %w", remote.GetHttpUri().GetUri(), err)
	}

	// Rewrite remote fetch to local file.
	vm.Code = &core.AsyncDataSource{
		Specifier: &core.AsyncDataSource_Local{
			Local: &core.DataSource{
				Specifier: &core.DataSource_Filename{
					Filename: f,
				},
			},
		},
	}
	return nil
}
