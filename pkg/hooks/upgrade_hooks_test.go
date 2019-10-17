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

package hooks

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/manifest"
	"istio.io/operator/pkg/util"
)

var (
	err1 = fmt.Errorf("err1")
	err2 = fmt.Errorf("err2")
	err3 = fmt.Errorf("err3")
)

func h1(_ manifest.ExecClient, _, _ *v1alpha2.IstioControlPlaneSpec) util.Errors {
	return util.NewErrs(err1)
}
func h2(_ manifest.ExecClient, _, _ *v1alpha2.IstioControlPlaneSpec) util.Errors {
	return util.NewErrs(err2)
}
func h3(_ manifest.ExecClient, _, _ *v1alpha2.IstioControlPlaneSpec) util.Errors {
	return util.NewErrs(err3)
}

func TestRunUpgradeHooks(t *testing.T) {
	testUpgradeHooks := []hookVersionMapping{
		{
			sourceVersionConstraint: ">0",
			targetVersionConstraint: ">0",
			hooks:                   []hook{h1},
		},
		{
			sourceVersionConstraint: ">=1.3, <1.4",
			targetVersionConstraint: ">=1.5",
			hooks:                   []hook{h2},
		},
		{
			sourceVersionConstraint: ">=1.5",
			targetVersionConstraint: ">=1.5",
			hooks:                   []hook{h3},
		},
	}

	malformedStr := "Malformed version: bad ver"
	tests := []struct {
		desc      string
		sourceVer string
		targetVer string
		dryRun    bool
		wantErrs  util.Errors
	}{
		{
			desc:      "bad ver",
			sourceVer: "bad ver",
			targetVer: "1.3",
			wantErrs:  util.Errors{errors.New(malformedStr)},
		},
		{
			desc:      "h1",
			sourceVer: "1.2",
			targetVer: "1.3",
			wantErrs:  util.Errors{err1},
		},
		{
			desc:      "h2 boundary outside",
			sourceVer: "1.4",
			targetVer: "1.5",
			wantErrs:  util.Errors{err1},
		},
		{
			desc:      "h2 boundary inside",
			sourceVer: "1.3",
			targetVer: "1.5",
			wantErrs:  util.Errors{err1, err2},
		},
		{
			desc:      "h2 range",
			sourceVer: "1.3.5",
			targetVer: "1.6.1",
			wantErrs:  util.Errors{err1, err2},
		},
		{
			desc:      "h3 range",
			sourceVer: "1.5.2",
			targetVer: "1.5.1",
			wantErrs:  util.Errors{err1, err3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			hc := HookCommonParams{
				SourceVer: tt.sourceVer,
				TargetVer: tt.targetVer,
			}
			gotErrs := runUpgradeHooks(testUpgradeHooks, nil, &hc, tt.dryRun)
			if !util.EqualErrors(gotErrs, tt.wantErrs) {
				t.Errorf("%s: got: %s, wantErrs: %s", tt.desc, gotErrs.String(), tt.wantErrs.String())
			}
		})
	}
}
