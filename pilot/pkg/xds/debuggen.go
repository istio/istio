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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
)

const (
	// TypeDebug requests debug info from istio, a secured implementation for istio debug interface
	TypeDebug = "istio.io/debug"
	debugURL  = "http://localhost"
)

var activeNamespaceDebuggers = map[string]struct{}{
	"config_dump": {},
	"ndsz":        {},
	"edsz":        {},
}

// DebugGen is a Generator for istio debug info
type DebugGen struct {
	Server *DiscoveryServer
}

func NewDebugGen(s *DiscoveryServer) *DebugGen {
	return &DebugGen{
		Server: s,
	}
}

// Generate XDS debug responses according to the incoming debug request
func (dg *DebugGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates *model.PushRequest) (model.Resources, error) {
	res := []*any.Any{}
	var err error
	var out []byte
	if w.ResourceNames == nil {
		return res, fmt.Errorf("debug type is required")
	}
	if len(w.ResourceNames) != 1 {
		return res, fmt.Errorf("only one debug request is allowed")
	}
	resourceName := w.ResourceNames[0]
	u, _ := url.Parse(resourceName)
	debugType := u.Path
	identity := proxy.VerifiedIdentity
	if identity.Namespace != features.PodNamespaceVar.Get() {
		shouldAllow := false
		if _, ok := activeNamespaceDebuggers[debugType]; ok {
			shouldAllow = true
		}
		if !shouldAllow {
			res = append(res, &any.Any{
				// nolint: staticcheck
				TypeUrl: TypeDebug,
				Value:   []byte(fmt.Sprintf("the debug info is not available for current identity: %q", identity)),
			})
			return res, nil
		}
	}
	debugURL := debugURL + debugServerPort + "/debug/" + resourceName
	resp, err := http.Get(debugURL)
	if err != nil {
		return nil, err
	}
	out, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if out != nil {
		res = append(res, &any.Any{
			TypeUrl: TypeDebug,
			Value:   out,
		})
	}
	return res, nil
}
