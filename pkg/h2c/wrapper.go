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

package h2c

import (
	"net/http"
	"net/textproto"

	"golang.org/x/net/http/httpguts"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c" // nolint: depguard
)

// NewHandler returns an http.Handler that wraps h, intercepting any h2c
// traffic. See h2c.NewHandler for details.
// Unlike the normal handler, this handler prevents h2c Upgrades, which are not safe in Go's implementation;
// see https://github.com/golang/go/issues/56352.
// This means we allow only HTTP/1.1 or HTTP/2 prior knowledge.
func NewHandler(h http.Handler, s *http2.Server) http.Handler {
	return denyH2cUpgrade(h2c.NewHandler(h, s))
}

func denyH2cUpgrade(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isH2CUpgrade(r.Header) {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("h2c upgrade not allowed"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

func isH2CUpgrade(h http.Header) bool {
	return httpguts.HeaderValuesContainsToken(h[textproto.CanonicalMIMEHeaderKey("Upgrade")], "h2c") &&
		httpguts.HeaderValuesContainsToken(h[textproto.CanonicalMIMEHeaderKey("Connection")], "HTTP2-Settings")
}
