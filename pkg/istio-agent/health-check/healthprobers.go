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

package health_check

type Prober interface {
	// Probe will healthcheck and return whether or not the target is healthy.
	Probe() (bool, error)
}

type HTTPProber struct {
	// api HTTPProbe protobuf msg
}

func (h *HTTPProber) Probe() (bool, error) {
	return false, nil
}

type TCPProber struct {
	// api TCPProbe protobuf msg
}

func (t *TCPProber) Probe() (bool, error) {
	return false, nil
}

type ExecProber struct {
	// api ExecProbe protobuf msg
}

func (e *ExecProber) Probe() (bool, error) {
	return false, nil
}