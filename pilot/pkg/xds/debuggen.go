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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	any "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/model"
)

const (
	// TypeDebug requests debug info from istio, a secured implementation for istio debug interface
	TypeDebug = "istio.io/debug"
)

var activeNamespaceDebuggers = map[string]struct{}{
	"config_dump": {},
	"ndsz":        {},
	"edsz":        {},
}

// DebugGen is a Generator for istio debug info
type DebugGen struct {
	Server          *DiscoveryServer
	SystemNamespace string
	DebugMux        *http.ServeMux
}

type ResponseCapture struct {
	body        *bytes.Buffer
	header      map[string]string
	wroteHeader bool
}

func (r ResponseCapture) Header() http.Header {
	header := make(http.Header)
	for k, v := range r.header {
		header.Set(k, v)
	}
	return header
}

func (r ResponseCapture) Write(i []byte) (int, error) {
	return r.body.Write(i)
}

func (r ResponseCapture) WriteHeader(statusCode int) {
	r.header["statusCode"] = strconv.Itoa(statusCode)
}

func NewResponseCapture() *ResponseCapture {
	return &ResponseCapture{
		header:      make(map[string]string),
		body:        new(bytes.Buffer),
		wroteHeader: false,
	}
}

func NewDebugGen(s *DiscoveryServer, systemNamespace string) *DebugGen {
	return &DebugGen{
		Server:          s,
		SystemNamespace: systemNamespace,
	}
}

// Generate XDS debug responses according to the incoming debug request
func (dg *DebugGen) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	res := model.Resources{}
	var buffer bytes.Buffer
	if proxy.VerifiedIdentity == nil {
		log.Warnf("proxy %s is not authorized to receive debug. Ensure you are connecting over TLS port and are authenticated.", proxy.ID)
		return nil, model.DefaultXdsLogDetails, fmt.Errorf("authentication required")
	}
	if w.ResourceNames == nil {
		return res, model.DefaultXdsLogDetails, fmt.Errorf("debug type is required")
	}
	if len(w.ResourceNames) != 1 {
		return res, model.DefaultXdsLogDetails, fmt.Errorf("only one debug request is allowed")
	}
	resourceName := w.ResourceNames[0]
	u, _ := url.Parse(resourceName)
	debugType := u.Path
	identity := proxy.VerifiedIdentity
	if identity.Namespace != dg.SystemNamespace {
		shouldAllow := false
		if _, ok := activeNamespaceDebuggers[debugType]; ok {
			shouldAllow = true
		}
		if !shouldAllow {
			return res, model.DefaultXdsLogDetails, fmt.Errorf("the debug info is not available for current identity: %q", identity)
		}
	}
	debugURL := "/debug/" + resourceName
	hreq, _ := http.NewRequest(http.MethodGet, debugURL, nil)
	handler, _ := dg.DebugMux.Handler(hreq)
	response := NewResponseCapture()
	handler.ServeHTTP(response, hreq)
	if response.wroteHeader && len(response.header) >= 1 {
		header, _ := json.Marshal(response.header)
		buffer.Write(header)
	}
	buffer.Write(response.body.Bytes())
	res = append(res, &discovery.Resource{
		Name: resourceName,
		Resource: &any.Any{
			TypeUrl: TypeDebug,
			Value:   buffer.Bytes(),
		},
	})
	return res, model.DefaultXdsLogDetails, nil
}
