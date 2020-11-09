//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package common

import "net/http"

const (
	webSocketHeaderKey   = "testwebsocket"
	webSocketHeaderValue = "enabled"
)

// IsWebSocketRequest indicates whether the request contains the header that triggers an upgrade to the WebSocket protocol.
func IsWebSocketRequest(r *http.Request) bool {
	return r.Header.Get(webSocketHeaderKey) == webSocketHeaderValue
}

// SetWebSocketHeader sets the header on the request which will trigger an upgrade to the WebSocket protocol.
func SetWebSocketHeader(headers http.Header) {
	headers.Set(webSocketHeaderKey, webSocketHeaderValue)
}
