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

package galley_test

import (
	"fmt"
	"testing"

	mcp "istio.io/api/mcp/v1alpha1"

	"github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework/components/galley"
)

func TestSingleObjectSnapshotValidator(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	actuals := []*galley.SnapshotObject{
		{
			TypeURL: "testURL",
			Metadata: &mcp.Metadata{
				Name: "testNS/testName",
			},
		},
		{
			TypeURL: "testURL2",
			Metadata: &mcp.Metadata{
				Name: "testNS2/testName2",
			},
		},
	}

	validatorFunc := galley.NewSingleObjectSnapshotValidator("testNS", testValidator)
	err := validatorFunc(actuals)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	actuals = append(actuals, &galley.SnapshotObject{
		TypeURL: "testURL",
		Metadata: &mcp.Metadata{
			Name: "testNS/otherTestName",
		},
	})
	err = validatorFunc(actuals)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err).To(gomega.Equal(fmt.Errorf("expected 1 resource, found %d", len(actuals))))

}

func testValidator(ns string, actual *galley.SnapshotObject) error {
	return nil
}
