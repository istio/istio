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

package status

import (
	"reflect"
	"testing"

	"github.com/onsi/gomega"

	"gopkg.in/yaml.v2"
)

func TestReportSerialization(t *testing.T) {
	in := DistributionReport{
		Reporter:       "Me",
		DataPlaneCount: 10,
		InProgressResources: map[string]int{
			(&Resource{
				Name:      "water",
				Namespace: "default",
			}).String(): 1,
		},
	}
	outbytes, err := yaml.Marshal(in)
	gomega.RegisterTestingT(t)
	gomega.Expect(err).To(gomega.BeNil())
	out := DistributionReport{}
	err = yaml.Unmarshal(outbytes, &out)
	gomega.Expect(err).To(gomega.BeNil())
	if !reflect.DeepEqual(out, in) {
		t.Errorf("Report Serialization mutated the Report. got = %v, want %v", out, in)
	}
}
