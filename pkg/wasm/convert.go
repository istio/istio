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
	rbac "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/hashicorp/go-multierror"
	anypb "google.golang.org/protobuf/types/known/anypb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/config/xds"
)

var allowTypedConfig = protoconv.MessageToAny(&rbac.RBAC{})

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
			resource := resources[i]
			extConfig := &core.TypedExtensionConfig{}
			if err := resource.UnmarshalTo(extConfig); err != nil {
				wasmConfigConversionCount.
					With(resultTag.Value(unmarshalFailure)).
					Increment()
				convertErrs[i] = err
				return
			}

			err := rewriteWasmExtensions(extConfig, cache)
			if err != nil {
				wasmConfigConversionCount.
					With(resultTag.Value(unmarshalFailure)).
					Increment()
				convertErrs[i] = err
				return
			}
			nec, _ := anypb.New(extConfig)
			resources[i] = nec
		}(i)
	}

	wg.Wait()
	err := multierror.Append(nil, convertErrs...).ErrorOrNil()
	if err != nil {
		wasmLog.Errorf("convert the wasm config: %v", err)
	}
	return err
}

// rewriteWasmExtensions updates the extension config to use local file instead of remote fetch for all wasm filters.
// It does an in-place update of WASM config.
// Currently we only support a WASM filter per extension config.
func rewriteWasmExtensions(ec *core.TypedExtensionConfig, cache Cache) error {
	wasmHTTPFilterConfig := &wasm.Wasm{}

	// Wasm filter can be configured using typed struct and Wasm filter type
	switch {
	case ec.GetTypedConfig() == nil:
		return fmt.Errorf("typed extension config %+v does not contain any typed config", ec)
		// TODO: Currently only WASM HTTP filter is supported. Extend it to Network filter when ECDS is supported for network filters.
	case ec.GetTypedConfig().TypeUrl == xds.WasmHTTPFilterType:
		if err := ec.GetTypedConfig().UnmarshalTo(wasmHTTPFilterConfig); err != nil {
			return fmt.Errorf("failed to unmarshal extension config resource into Wasm HTTP filter: %w", err)
		}
	case ec.GetTypedConfig().TypeUrl == xds.TypedStructType:
		typedStruct := &udpa.TypedStruct{}
		wasmTypedConfig := ec.GetTypedConfig()
		if err := wasmTypedConfig.UnmarshalTo(typedStruct); err != nil {
			return fmt.Errorf("failed to unmarshal typed config for wasm filter: %w", err)
		}

		if typedStruct.TypeUrl == xds.WasmHTTPFilterType {
			if err := conversion.StructToMessage(typedStruct.Value, wasmHTTPFilterConfig); err != nil {
				return fmt.Errorf("failed to convert extension config struct %+v to Wasm HTTP filter", typedStruct)
			}
		} else {
			// This is not a Wasm filter.
			wasmLog.Debugf("typed extension config %+v does not contain wasm http filter", typedStruct)
			return nil
		}
	default:
		// This is not a Wasm filter.
		wasmLog.Debugf("cannot find typed config or typed struct in %+v", ec)
		// This is not a wasm config. Let's bypass it and increment metric.
		wasmConfigConversionCount.
			With(resultTag.Value(noRemoteLoad)).
			Increment()
		return nil
	}

	if wasmHTTPFilterConfig.Config.GetVmConfig().GetCode().GetRemote() == nil {
		if wasmHTTPFilterConfig.Config.GetVmConfig().GetCode().GetLocal() == nil {
			return fmt.Errorf("no remote and local load found in Wasm HTTP filter %+v", wasmHTTPFilterConfig)
		}
		// This has a local Wasm. Let's bypass it.
		wasmLog.Debugf("no remote load found in Wasm HTTP filter %+v", wasmHTTPFilterConfig)
		wasmConfigConversionCount.
			With(resultTag.Value(noRemoteLoad)).
			Increment()
		return nil
	}
	// ec.Name is resourceName.
	// https://github.com/istio/istio/blob/9ea7ad532a9cc58a3564143d41ac89a61aaa8058/pilot/pkg/networking/core/v1alpha3/extension/wasmplugin.go#L103
	localWasm, err := rewriteWasmFromRemoteToLocal(ec.Name, wasmHTTPFilterConfig, cache)
	if err != nil {
		return err
	}
	ec.TypedConfig = localWasm
	return nil
}

func rewriteWasmFromRemoteToLocal(name string, wasmHTTPFilterConfig *wasm.Wasm, cache Cache) (*anypb.Any, error) {
	status := conversionSuccess
	defer func() {
		wasmConfigConversionCount.
			With(resultTag.Value(status)).
			Increment()
	}()

	vm := wasmHTTPFilterConfig.Config.GetVmConfig()
	envs := vm.GetEnvironmentVariables()
	var pullSecret []byte
	pullPolicy := extensions.PullPolicy_UNSPECIFIED_POLICY
	resourceVersion := ""
	if envs != nil {
		if sec, found := envs.KeyValues[model.WasmSecretEnv]; found {
			if sec == "" {
				status = fetchFailure
				return nil, fmt.Errorf("cannot fetch Wasm module %v: missing image pulling secret", wasmHTTPFilterConfig.Config.Name)
			}
			pullSecret = []byte(sec)
		}

		if ps, found := envs.KeyValues[model.WasmPolicyEnv]; found {
			if p, found := extensions.PullPolicy_value[ps]; found {
				pullPolicy = extensions.PullPolicy(p)
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
		status = missRemoteFetchHint
		return nil, fmt.Errorf("wasm remote fetch %+v does not have httpUri specified", remote)
	}
	// checksum sent by istiod can be "nil" if not set by user - magic value used to avoid unmarshaling errors
	if remote.Sha256 == "nil" {
		remote.Sha256 = ""
	}
	// Default timeout. Without this if user does not specify a timeout in the config, it fails with deadline exceeded
	// while building transport in go container.
	timeout := time.Second * 5
	if remote.GetHttpUri().Timeout != nil {
		timeout = remote.GetHttpUri().Timeout.AsDuration()
	}
	f, err := cache.Get(httpURI.GetUri(), GetOptions{
		Checksum:        remote.Sha256,
		ResourceName:    name,
		ResourceVersion: resourceVersion,
		RequestTimeout:  timeout,
		PullSecret:      pullSecret,
		PullPolicy:      pullPolicy,
	})
	if err != nil {
		status = fetchFailure
		if wasmHTTPFilterConfig.GetConfig().GetFailOpen() {
			return allowTypedConfig, nil
		}
		return nil, fmt.Errorf("cannot fetch Wasm module %v: %w", remote.GetHttpUri().GetUri(), err)
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
	wasmTypedConfig, err := anypb.New(wasmHTTPFilterConfig)
	if err != nil {
		status = marshalFailure
		return nil, fmt.Errorf("failed to marshal new wasm HTTP filter %+v to protobuf Any: %w", wasmHTTPFilterConfig, err)
	}
	return wasmTypedConfig, nil
}
