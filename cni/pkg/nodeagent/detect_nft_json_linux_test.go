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

package nodeagent

import (
	"context"
	"os/exec"
	"testing"
)

func TestDetectNftJSONSupport(t *testing.T) {
	original := nftJSONProbeCommandFn
	t.Cleanup(func() { nftJSONProbeCommandFn = original })

	cases := []struct {
		name    string
		cmd     *exec.Cmd
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "json supported, exit 0",
			cmd:    exec.Command("sh", "-c", "echo '{\"nftables\":[]}'; exit 0"),
			wantOK: true,
		},
		{
			name:    "json not compiled in",
			cmd:     exec.Command("sh", "-c", "echo 'JSON support not compiled-in' 1>&2; exit 1"),
			wantOK:  false,
			wantErr: false,
		},
		{
			name:    "marker on stdout still detected",
			cmd:     exec.Command("sh", "-c", "echo 'JSON support not compiled-in'; exit 1"),
			wantOK:  false,
			wantErr: false,
		},
		{
			name:    "other failure surfaces error",
			cmd:     exec.Command("sh", "-c", "echo 'permission denied' 1>&2; exit 1"),
			wantOK:  false,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd
			nftJSONProbeCommandFn = func(ctx context.Context) *exec.Cmd { return cmd }
			ok, err := detectNftJSONSupport()
			if ok != tc.wantOK {
				t.Errorf("got ok=%v, want %v", ok, tc.wantOK)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
