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
	"io"
	"net/http"

	adminv3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	"google.golang.org/protobuf/encoding/protojson"

	"istio.io/istio/pkg/util/configdump"
	"istio.io/istio/pkg/util/sets"
)

type ModuleGetter interface {
	ListUsingModules() (sets.String, error)
}

type LocalModuleGetter struct {
	AdminPort     int32
	LocalHostAddr string
}

func NewLocalModuleGetter(adminPort int32, localhostAddr string) ModuleGetter {
	return &LocalModuleGetter{
		AdminPort:     adminPort,
		LocalHostAddr: localhostAddr,
	}
}

func (g *LocalModuleGetter) ListUsingModules() (sets.String, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d/config_dump", g.LocalHostAddr, g.AdminPort), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	cd := &adminv3.ConfigDump{}
	if err := protojson.Unmarshal(b, cd); err != nil {
		return nil, err
	}

	w := configdump.Wrapper{
		ConfigDump: cd,
	}

	ecdsDump, err := w.GetEcdsConfigDump()
	if err != nil {
		return nil, err
	}
	modules := sets.New[string]()
	for _, ec := range ecdsDump.EcdsFilters {
		c := &core.TypedExtensionConfig{}
		if err := ec.GetEcdsFilter().UnmarshalTo(c); err != nil {
			wasmLog.Warnf("unmarshall TypedExtensionConfig fail: %v", err)
			continue
		}

		httpWasm := &wasm.Wasm{}
		if err := c.GetTypedConfig().UnmarshalTo(httpWasm); err != nil {
			wasmLog.Warnf("unmarshall http WASM fail: %v", err)
			continue
		}

		modulePath := httpWasm.Config.GetVmConfig().GetCode().GetLocal().GetFilename()
		if modulePath != "" {
			modules.Insert(modulePath)
		}
	}

	return modules, nil
}
