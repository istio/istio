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

package endpoint

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"os"

	"istio.io/istio/pkg/test/echo/common/response"
)

func listenOnPort(port int) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, 0, err
	}

	port = ln.Addr().(*net.TCPAddr).Port
	return ln, port, nil
}

func listenOnPortTLS(port int, cfg *tls.Config) (net.Listener, int, error) {
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), cfg)
	if err != nil {
		return nil, 0, err
	}

	port = ln.Addr().(*net.TCPAddr).Port
	return ln, port, nil
}

func listenOnUDS(uds string) (net.Listener, error) {
	_ = os.Remove(uds)
	ln, err := net.Listen("unix", uds)
	if err != nil {
		return nil, err
	}

	return ln, nil
}

// nolint: interfacer
func writeField(out *bytes.Buffer, field response.Field, value string) {
	_, _ = out.WriteString(string(field) + "=" + value + "\n")
}
