// Copyright 2017 Istio Authors
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

package runtime

import "testing"

func TestResourceType_String(t *testing.T) {

	for _, tc := range []struct {
		res ResourceType
		ans string
	}{
		{
			res: ResourceType{
				protocolHTTP,
				methodReport,
			},
			ans: "ResourceType:{HTTP /Report }",
		},
		{
			res: defaultResourcetype(),
			ans: "ResourceType:{HTTP /Check Report Preprocess}",
		},
		{
			res: ResourceType{
				protocolTCP | protocolHTTP,
				methodQuota,
			},
			ans: "ResourceType:{HTTP TCP /Quota}",
		},
	} {
		t.Run(tc.ans, func(t *testing.T) {
			if tc.ans != tc.res.String() {
				t.Fatalf("Got <%s>, want <%s>", tc.res.String(), tc.ans)
			}
		})

	}

}
