// Copyright 2017 Istio Authors
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

package exporter

import (
	"os"

	"istio.io/istio/pilot/pkg/registeragent/exporter/default"
	"istio.io/istio/pilot/pkg/registeragent/exporter/hsf"
	"istio.io/istio/pkg/log"
)

func RpcInfoExporterFactory() (r RpcAcutator) {
	rpcType := os.Getenv("RPC_TYPE")
	log.Infof("rpcType: %s", rpcType)
	switch rpcType {
	case "HSF":
		r = hsf.NewRpcInfoExporter()
	default:
		r = _default.NewRpcInfoExporter()
	}
	return r
}
