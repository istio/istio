//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package creds

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestAttachCobraFlags(t *testing.T) {
	o := DefaultOptions()
	cmd := &cobra.Command{}
	o.AttachCobraFlags(cmd)

	cases := []struct {
		name      string
		wantValue string
	}{
		{
			name:      "certFile",
			wantValue: o.CertificateFile,
		},
		{
			name:      "keyFile",
			wantValue: o.KeyFile,
		},
		{
			name:      "caCertFile",
			wantValue: o.CACertificateFile,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(tt *testing.T) {
			got := cmd.Flag(c.name)
			if got == nil {
				tt.Fatal("flag not found")
			}
			if got.Value.String() != c.wantValue {
				tt.Fatalf("wrong default value: got %q want %q", got.Value.String(), c.wantValue)
			}
		})
	}
}
