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
	"sync"
	"time"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"go.uber.org/atomic"
	any "google.golang.org/protobuf/types/known/anypb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/xds"
)

// MaybeConvertWasmExtensionConfig converts any presence of module remote download to local file.
// It downloads the Wasm module and stores the module locally in the file system.
func MaybeConvertWasmExtensionConfig(resources []*any.Any, cache Cache) bool {
	var wg sync.WaitGroup
	numResources := len(resources)
	wg.Add(numResources)
	sendNack := atomic.NewBool(false)
	startTime := time.Now()
	defer func() {
		wasmConfigConversionDuration.Record(float64(time.Since(startTime).Milliseconds()))
	}()

	for i := 0; i < numResources; i++ {
		go func(i int) {
			defer wg.Done()

			newExtensionConfig, nack := convert(resources[i], cache)
			if nack {
				sendNack.Store(true)
				return
			}
			resources[i] = newExtensionConfig
		}(i)
	}

	wg.Wait()
	return sendNack.Load()
}

func convert(resource *any.Any, cache Cache) (newExtensionConfig *any.Any, sendNack bool) {
	ec := &core.TypedExtensionConfig{}
	newExtensionConfig = resource
	sendNack = false
	status := noRemoteLoad
	defer func() {
		wasmConfigConversionCount.
			With(resultTag.Value(status)).
			Increment()
	}()
	if err := resource.UnmarshalTo(ec); err != nil {
		wasmLog.Debugf("failed to unmarshal extension config resource: %v", err)
		return
	}

	wasmHTTPFilterConfig := &wasm.Wasm{}
	// Wasm filter can be configured using typed struct and Wasm filter type
	if ec.GetTypedConfig() != nil && ec.GetTypedConfig().TypeUrl == xds.WasmHTTPFilterType {
		err := ec.GetTypedConfig().UnmarshalTo(wasmHTTPFilterConfig)
		if err != nil {
			wasmLog.Debugf("failed to unmarshal extension config resource into Wasm HTTP filter: %v", err)
			return
		}
	} else if ec.GetTypedConfig() == nil || ec.GetTypedConfig().TypeUrl != xds.TypedStructType {
		wasmLog.Debugf("cannot find typed struct in %+v", ec)
		return
	} else {
		wasmStruct := &udpa.TypedStruct{}
		wasmTypedConfig := ec.GetTypedConfig()
		if err := wasmTypedConfig.UnmarshalTo(wasmStruct); err != nil {
			wasmLog.Debugf("failed to unmarshal typed config for wasm filter: %v", err)
			return
		}

		if wasmStruct.TypeUrl != xds.WasmHTTPFilterType {
			wasmLog.Debugf("typed extension config %+v does not contain wasm http filter", wasmStruct)
			return
		}

		if err := conversion.StructToMessage(wasmStruct.Value, wasmHTTPFilterConfig); err != nil {
			wasmLog.Debugf("failed to convert extension config struct %+v to Wasm HTTP filter", wasmStruct)
			return
		}
	}

	if wasmHTTPFilterConfig.Config.GetVmConfig().GetCode().GetRemote() == nil {
		wasmLog.Debugf("no remote load found in Wasm HTTP filter %+v", wasmHTTPFilterConfig)
		return
	}

	// Wasm plugin configuration has remote load. From this point, any failure should result as a Nack,
	// unless the plugin is marked as fail open.
	failOpen := wasmHTTPFilterConfig.Config.GetFailOpen()
	sendNack = !failOpen
	status = conversionSuccess

	vm := wasmHTTPFilterConfig.Config.GetVmConfig()
	envs := vm.GetEnvironmentVariables()
	var pullSecret []byte
	pullPolicy := extensions.PullPolicy_UNSPECIFIED_POLICY
	resourceVersion := ""
	if envs != nil {
		if sec, found := envs.KeyValues[model.WasmSecretEnv]; found {
			if sec == "" {
				status = fetchFailure
				wasmLog.Errorf("cannot fetch Wasm module %v: missing image pulling secret", wasmHTTPFilterConfig.Config.Name)
				return
			}
			pullSecret = []byte(sec)
		}
		// Strip all internal env variables from VM env variable.
		// These env variables are added by Istio control plane and meant to be consumed by the agent for image pulling control,
		// thus should not be leaked to Envoy or the Wasm extension runtime.
		delete(envs.KeyValues, model.WasmSecretEnv)
		if len(envs.KeyValues) == 0 {
			if len(envs.HostEnvKeys) == 0 {
				vm.EnvironmentVariables = nil
			} else {
				envs.KeyValues = nil
			}
		}

		if ps, found := envs.KeyValues[model.WasmPolicyEnv]; found {
			if p, found := extensions.PullPolicy_value[ps]; found {
				pullPolicy = extensions.PullPolicy(p)
			}
		}

		resourceVersion = envs.KeyValues[model.WasmResourceVersionEnv]
	}
	remote := vm.GetCode().GetRemote()
	httpURI := remote.GetHttpUri()
	if httpURI == nil {
		status = missRemoteFetchHint
		wasmLog.Errorf("wasm remote fetch %+v does not have httpUri specified", remote)
		return
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
	f, err := cache.Get(httpURI.GetUri(), remote.Sha256, wasmHTTPFilterConfig.Config.Name, resourceVersion, timeout, pullSecret, pullPolicy)
	if err != nil {
		status = fetchFailure
		wasmLog.Errorf("cannot fetch Wasm module %v: %v", remote.GetHttpUri().GetUri(), err)
		return
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

	wasmTypedConfig, err := any.New(wasmHTTPFilterConfig)
	if err != nil {
		status = marshalFailure
		wasmLog.Errorf("failed to marshal new wasm HTTP filter %+v to protobuf Any: %v", wasmHTTPFilterConfig, err)
		return
	}
	ec.TypedConfig = wasmTypedConfig
	wasmLog.Debugf("new extension config resource %+v", ec)

	nec, err := any.New(ec)
	if err != nil {
		status = marshalFailure
		wasmLog.Errorf("failed to marshal new extension config resource: %v", err)
		return
	}

	// At this point, we are certain that wasm module has been downloaded and config is rewritten.
	// ECDS has been rewritten successfully and should not nack.
	newExtensionConfig = nec
	sendNack = false
	return
}
