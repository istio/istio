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

package xds

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// disableHBONEConnectWaitIfUnsupported ignores the flag for proxies without the istio_hbone extensions, which reject CONNECT wait config.
func disableHBONEConnectWaitIfUnsupported(proxy *model.Proxy, node *core.Node) {
	if proxy.Metadata == nil || !bool(proxy.Metadata.EnableHBONEConnectWait) {
		return
	}
	if util.NodeSupportsHBONEConnectWait(node) {
		return
	}
	log.Warnf("%s: ignoring ENABLE_HBONE_CONNECT_WAIT: proxy does not report the required extensions %v",
		proxy.ID, util.HBONEConnectWaitExtensions)
	proxy.Metadata.EnableHBONEConnectWait = false
}
