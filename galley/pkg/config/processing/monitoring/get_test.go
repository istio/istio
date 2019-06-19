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

package monitoring

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
)

func TestGet(t *testing.T) {
	g := NewGomegaWithT(t)

	r := &reporter{}

	mu.Lock()
	g.Expect(instance).To(BeNil())
	oldPatchTable := getPatchTable
	getPatchTable.createOpenCensusReporter = func() (*reporter, error) {
		return r, nil
	}
	mu.Unlock()

	defer func() {
		getPatchTable = oldPatchTable
		mu.Lock()
		instance = nil
		mu.Unlock()
	}()

	rep, err := Get()
	g.Expect(err).To(BeNil())
	g.Expect(rep).To(BeEquivalentTo(r))
}

func TestGet_Err(t *testing.T) {
	g := NewGomegaWithT(t)

	mu.Lock()
	g.Expect(instance).To(BeNil())
	oldPatchTable := getPatchTable
	getPatchTable.createOpenCensusReporter = func() (*reporter, error) {
		return nil, fmt.Errorf("some err")
	}
	mu.Unlock()

	defer func() {
		getPatchTable = oldPatchTable
		mu.Lock()
		instance = nil
		mu.Unlock()
	}()

	_, err := Get()
	g.Expect(err).NotTo(BeNil())
}

func TestGetForReal(t *testing.T) {
	g := NewGomegaWithT(t)

	mu.Lock()
	g.Expect(instance).To(BeNil())
	mu.Unlock()

	defer func() {
		mu.Lock()
		err := instance.(*reporter).Close()
		g.Expect(err).To(BeNil())
		instance = nil
		mu.Unlock()
	}()

	rep, err := Get()
	g.Expect(err).To(BeNil())
	g.Expect(rep).NotTo(BeNil())
}
