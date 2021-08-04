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

package dependencies

import (
	"bytes"
	"testing"
)

func TestIsXTablesLockError(t *testing.T) {
	xtableError := []struct {
		name    string
		errMsg  string
		errCode XTablesExittype
		want    bool
	}{
		{
			name:    "no error",
			errMsg:  "",
			errCode: 0,
			want:    false,
		},
		{
			name:    "error message mismatch",
			errMsg:  "some error",
			errCode: XTablesResourceProblem,
			want:    false,
		},
		{
			name:    "error code mismatch",
			errMsg:  "some error",
			errCode: XTablesOtherProblem,
			want:    false,
		},
		{
			name:    "is xtables lock error",
			errMsg:  "Another app is currently holding the xtables lock.",
			errCode: XTablesResourceProblem,
			want:    true,
		},
	}
	for _, tt := range xtableError {
		t.Run(tt.name, func(t *testing.T) {
			bs := bytes.NewBufferString(tt.errMsg)
			if got, want := isXTablesLockError(bs, int(tt.errCode)), tt.want; got != want {
				t.Errorf("xtables lock error got %v, want %v", got, want)
			}
		})
	}
}
