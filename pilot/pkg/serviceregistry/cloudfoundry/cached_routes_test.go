// Copyright 2018 Istio Authors
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

package cloudfoundry_test

import (
	"fmt"
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry/fakes"
)

func TestGet(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	client := &fakes.CopilotClient{}
	logger := &fakes.Logger{}

	cachedRoutes := cloudfoundry.NewCachedRoutes(client, logger, "30s")

	g.Consistently(func() int {
		cachedRoutes.Get()
		return client.RoutesCallCount()
	}).Should(gomega.Equal(1))

	g.Expect(fmt.Sprint(logger.InfoaArgsForCall(0)...)).To(gomega.Equal("retrieving routes from copilot"))
	for i := 1; i < client.RoutesCallCount(); i++ {
		g.Expect(fmt.Sprint(logger.InfoaArgsForCall(i)...)).To(gomega.Equal("retrieving routes from cache"))
	}
}
