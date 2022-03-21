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

package headers

import (
	"net/http"
)

// Builder for HTTP headers.
type Builder struct {
	headers http.Header
}

// New Builder for HTTP headers.
func New() *Builder {
	return &Builder{
		headers: make(http.Header),
	}
}

// With sets the given header value.
func (b *Builder) With(key, value string) *Builder {
	b.headers.Set(key, value)
	return b
}

// WithAuthz sets the Authorization header with the given token.
func (b *Builder) WithAuthz(token string) *Builder {
	if token != "" {
		return b.With(Authorization, "Bearer "+token)
	}
	return b
}

// WithHost sets the Host header to the given value.
func (b *Builder) WithHost(host string) *Builder {
	return b.With(Host, host)
}

// WithXForwardedFor sets the origin IP of the request.
func (b *Builder) WithXForwardedFor(ip string) *Builder {
	return b.With(XForwardedFor, ip)
}

func (b *Builder) Build() http.Header {
	return b.headers
}
