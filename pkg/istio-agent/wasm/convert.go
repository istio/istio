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
	wasmcache "istio.io/istio/pkg/wasm"
	"istio.io/pkg/log"

	udpa "github.com/cncf/udpa/go/udpa/type/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
)

var wasmFetchLog = log.RegisterScope("wasmfetch", "", 0)

func ConvertWasmFilter(l *listener.Listener, cache *wasmcache.Cache) (bool, bool) {
	needMarshal := false
	for _, c := range l.FilterChains {
		for _, nf := range c.Filters {
			// TODO: wasm network filters
			if nf.Name == wellknown.HTTPConnectionManager && nf.GetTypedConfig() != nil {
				httpConfig := &httppb.HttpConnectionManager{}
				nfc := nf.GetTypedConfig()
				nfc.UnmarshalTo(httpConfig)
				hcmNeedMarshal := false
				for _, hf := range httpConfig.HttpFilters {
					if hf.GetTypedConfig() == nil {
						continue
					}
					if hf.GetTypedConfig().TypeUrl != "type.googleapis.com/udpa.type.v1.TypedStruct" {
						continue
					}
					wasmts := &udpa.TypedStruct{}
					wasmTypedConfig := hf.GetTypedConfig()
					ptypes.UnmarshalAny(wasmTypedConfig, wasmts)

					if wasmts.TypeUrl != "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm" {
						continue
					}

					wasmFilterConfig := &wasm.Wasm{}
					conversion.StructToMessage(wasmts.Value, wasmFilterConfig)
					if wasmFilterConfig.Config.GetVmConfig().GetCode().GetRemote() == nil {
						continue
					}
					vm := wasmFilterConfig.Config.GetVmConfig()
					remote := vm.GetCode().GetRemote()
					f, err := cache.Get(remote.GetHttpUri().GetUri(), remote.GetSha256())
					if err != nil {
						wasmFetchLog.Errorf("cannot fetch Wasm module %v: %v", remote.GetHttpUri().GetUri(), err)
						return true, false
					}

					// rewrite code to reference the local file.
					vm.Code = &core.AsyncDataSource{
						Specifier: &core.AsyncDataSource_Local{
							Local: &core.DataSource{
								Specifier: &core.DataSource_Filename{
									Filename: f,
								},
							},
						},
					}

					wasmts.Value, err = conversion.MessageToStruct(wasmFilterConfig)
					if err != nil {
						wasmFetchLog.Errorf("failed to convert new wasm filter: %v", err)
						continue
					}
					wasmTypedConfig, err = ptypes.MarshalAny(wasmts)
					if err != nil {
						wasmFetchLog.Errorf("failed to marshal new wasm filter: %v", err)
						continue
					}
					hf.GetConfigType().(*httppb.HttpFilter_TypedConfig).TypedConfig = wasmTypedConfig
					hcmNeedMarshal = true
				}
				if !hcmNeedMarshal {
					continue
				}
				nnfc, err := ptypes.MarshalAny(httpConfig)
				if err != nil {
					wasmFetchLog.Errorf("failed to marshall new http filter chain: %v", err)
					continue
				}
				nf.GetConfigType().(*listener.Filter_TypedConfig).TypedConfig = nnfc
				needMarshal = true
			}
		}
	}

	return needMarshal, true
}
