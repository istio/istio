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
	"fmt"
	"net/http"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/pkg/ctrlz"
)

func TestCtrlz_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	o := ctrlz.DefaultOptions()
	c := NewCtrlz(o)
	err := c.Start()
	g.Expect(err).To(BeNil())
	defer c.Stop()

	url := fmt.Sprintf("http://%s:%d/", o.Address, o.Port)
	r, err := http.Get(url)
	g.Expect(err).To(BeNil())
	defer func() { _ = r.Body.Close() }()
	g.Expect(r.StatusCode).To(Equal(http.StatusOK))
}

func TestCtrlz_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	o := ctrlz.DefaultOptions()
	c1 := NewCtrlz(o)
	err := c1.Start()
	g.Expect(err).To(BeNil())
	defer c1.Stop()

	// Expect this one to fail
	c2 := NewCtrlz(o)
	err = c1.Start()
	g.Expect(err).NotTo(BeNil())
	defer c2.Stop()
}

func TestCtrlz_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	o := ctrlz.DefaultOptions()
	c := NewCtrlz(o)
	err := c.Start()
	g.Expect(err).To(BeNil())

	c.Stop()
	c.Stop()
}
