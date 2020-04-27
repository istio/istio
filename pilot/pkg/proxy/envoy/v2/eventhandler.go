// Copyright 2020 Istio Authors
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

package v2

// EventHandler allows for generic monitoring of xDS ACKS and disconnects, for the purpose of tracking
// Config distribution through the mesh.
type DistributionEventHandler interface {
	// RegisterEvent notifies the implementer of an xDS ACK, and must be non-blocking
	RegisterEvent(conID string, xdsType string, nonce string)
	RegisterDisconnect(s string, urls []string)
}
