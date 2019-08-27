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

package process

import (
	"errors"
	"testing"

	. "github.com/onsi/gomega"
)

func TestComponentFromFns(t *testing.T) {
	g := NewGomegaWithT(t)

	var started bool
	var stopped bool
	var startErr error
	c := ComponentFromFns(
		func() error {
			started = true
			return startErr
		},
		func() {
			stopped = true
		})

	err := c.Start()
	g.Expect(err).To(BeNil())
	g.Expect(started).To(BeTrue())

	c.Stop()
	g.Expect(stopped).To(BeTrue())

	startErr = errors.New("some error")
	err = c.Start()
	g.Expect(err).NotTo(BeNil())
}
