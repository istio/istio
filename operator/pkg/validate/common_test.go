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

package validate

import (
	"testing"

	"istio.io/istio/operator/pkg/util"
)

func TestValidateGlobalLoggingLevel(t *testing.T) {
	tests := []struct {
		desc     string
		val      interface{}
		wantErrs util.Errors
	}{
		{
			desc:     "empty logging level",
			val:      "",
			wantErrs: nil,
		},
		{
			desc:     "valid logging level setting",
			val:      "default:info",
			wantErrs: nil,
		},
		{
			desc:     "invalid logging level scope",
			val:      "ads:debug",
			wantErrs: makeErrors([]string{`validateGlobalLoggingLevel global.logging.level got logging level scope 'ads', want 'default' scope across all components`}),
		},
		{
			desc:     "invalid logging level value",
			val:      "default:xxx",
			wantErrs: makeErrors([]string{`validateGlobalLoggingLevel global.logging.level got logging level value 'xxx', want one of '["none" "error" "warning" "info" "debug"]'`}),
		},
		{
			desc:     "invalid logging level settings",
			val:      "ads:debug,default:debug",
			wantErrs: makeErrors([]string{`validateGlobalLoggingLevel global.logging.level got logging level scope 'ads', want 'default' scope across all components`}),
		},
	}
	path := "global.logging.level"
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			errs := validateGlobalLoggingLevel(util.PathFromString(path), tt.val)
			if gotErr, wantErr := errs, tt.wantErrs; !util.EqualErrors(gotErr, wantErr) {
				t.Errorf("validateGlobalLoggingLevel(%s)(%s, %v): gotErr:%s, wantErr:%s", tt.desc, path, tt.val, gotErr, wantErr)
			}
		})
	}
}
