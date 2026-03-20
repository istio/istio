// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Crediting the kgateway authors for the patterns used in this file, as well as some of the code

package agentgateway

import (
	"fmt"

	"github.com/agentgateway/agentgateway/go/api"
)

// ToAgwResource converts an internal representation to a resource for agentgateway
func ToAgwResource(t any) *api.Resource {
	switch tt := t.(type) {
	case AgwBind:
		return &api.Resource{Kind: &api.Resource_Bind{Bind: tt.Bind}}
	case AgwListener:
		return &api.Resource{Kind: &api.Resource_Listener{Listener: tt.Listener}}
	case AgwRoute:
		return &api.Resource{Kind: &api.Resource_Route{Route: tt.Route}}
	case AgwTCPRoute:
		return &api.Resource{Kind: &api.Resource_TcpRoute{TcpRoute: tt.TCPRoute}}
	case *api.Policy:
		return &api.Resource{Kind: &api.Resource_Policy{Policy: tt}}
	case *api.Resource:
		return tt
	}
	panic(fmt.Sprintf("unknown resource kind %T", t))
}
