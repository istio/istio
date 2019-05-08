// Copyright 2019 The OpenZipkin Authors
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

package zipkin

// Tag holds available types
type Tag string

// Common Tag values
const (
	TagHTTPMethod       Tag = "http.method"
	TagHTTPPath         Tag = "http.path"
	TagHTTPUrl          Tag = "http.url"
	TagHTTPRoute        Tag = "http.route"
	TagHTTPStatusCode   Tag = "http.status_code"
	TagHTTPRequestSize  Tag = "http.request.size"
	TagHTTPResponseSize Tag = "http.response.size"
	TagGRPCStatusCode   Tag = "grpc.status_code"
	TagSQLQuery         Tag = "sql.query"
	TagError            Tag = "error"
)

// Set a standard Tag with a payload on provided Span.
func (t Tag) Set(s Span, value string) {
	s.Tag(string(t), value)
}
