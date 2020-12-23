// Copyright Istio Authors.
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

package mesh

import (
	"fmt"
	"os"
	"testing"

	"istio.io/istio/operator/pkg/util/clog"
)

func TestVerifySupportedVersions(t *testing.T) {
	cases := []struct {
		currentVer string
		targetVer  string
		err        error
		isValid    bool
	}{
		{
			currentVer: "",
			targetVer:  "1.9.0",
			err:        fmt.Errorf("failed to parse the current version %v: Malformed version: ", ""),
			isValid:    false,
		},
		{
			currentVer: "master",
			targetVer:  "1.9.0",
			err:        fmt.Errorf("failed to parse the current version %v: Malformed version: ", "master"),
			isValid:    false,
		},
		{
			currentVer: "1.7.4",
			targetVer:  "1.8.0",
			err:        nil,
			isValid:    true,
		},
		{
			currentVer: "1.8.0",
			targetVer:  "1.9.0",
			err:        nil,
			isValid:    true,
		},
		{
			currentVer: "1.8.0",
			targetVer:  "1.7.4",
			err:        nil,
			isValid:    true,
		},
		{
			currentVer: "1.9.0",
			targetVer:  "",
			err:        fmt.Errorf("failed to parse the target version %v: Malformed version: %v", "", ""),
			isValid:    false,
		},
		{
			currentVer: "1.5.8",
			targetVer:  "1.8.0",
			err:        fmt.Errorf("upgrade is not supported before version: %v", upgradeSupportStart),
			isValid:    false,
		},
		{
			currentVer: "1.8.0",
			targetVer:  "1.8.0",
			err:        nil,
			isValid:    true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.currentVer), func(t *testing.T) {
			l := clog.NewConsoleLogger(os.Stdout, os.Stderr, nil)
			err := verifySupportedVersion(c.currentVer, c.targetVer, l)
			if c.isValid && err != nil {
				t.Errorf("(curr: %v)(target: %v)(%v)", c.currentVer, c.targetVer, err)
			}
		})
	}
}
