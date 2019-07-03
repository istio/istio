// Copyright 2019 Istio Authors
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

package components

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMonitoring_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer resetPatchTable()

	var addr string
	netListen = func(network, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		if err == nil {
			addr = l.Addr().String()
		}
		return l, err
	}

	m := NewMonitoring(0)
	err := m.Start()
	g.Expect(err).To(BeNil())

	url := fmt.Sprintf("http://%s%s", addr, metricsPath)
	r, err := http.Get(url)
	g.Expect(err).To(BeNil())
	defer func() { _ = r.Body.Close() }()
	g.Expect(r.StatusCode).To(Equal(http.StatusOK))

	defer m.Stop()
}

func TestMonitoring_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	defer resetPatchTable()
	netListen = func(network, address string) (net.Listener, error) {
		return nil, errors.New("ha")
	}

	m := NewMonitoring(0)
	err := m.Start()
	g.Expect(err).NotTo(BeNil())

	m.Stop() // nopanic
}

func TestMonitoring_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	m := NewMonitoring(0)
	err := m.Start()
	g.Expect(err).To(BeNil())
	m.Stop()
	m.Stop()
}
