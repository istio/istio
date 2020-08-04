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

package util

import (
	"errors"
	"testing"
)

func TestIsFilePath(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want bool
	}{
		{
			desc: "empty",
			in:   "",
			want: false,
		},
		{
			desc: "no-markers",
			in:   "foobar",
			want: false,
		},
		{
			desc: "with-slash",
			in:   "/home/bobby/go_rocks/main",
			want: true,
		},
		{
			desc: "with-period",
			in:   "istio.go",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := IsFilePath(tt.in), tt.want; !(got == want) {
				t.Errorf("%s: got %v, want: %v", tt.desc, got, want)
			}
		})
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want bool
		err  error
	}{
		{
			desc: "empty",
			in:   "",
			want: false,
			err:  nil,
		},
		{
			desc: "http-standard-url",
			in:   "http://localhost:3000",
			want: true,
			err:  nil,
		},
		{
			desc: "https-standard-url",
			in:   "https://golang.org/",
			want: true,
			err:  nil,
		},
		{
			desc: "ftp-url",
			in:   "ftp://gopher:gopwd@localhost:3000/go",
			want: false,
			err:  nil,
		},
		{
			desc: "tcp-discovery-url",
			in:   "tcp://127.0.0.1:80",
			want: false,
			err:  nil,
		},
		{
			desc: "empty-http",
			in:   "http://",
			want: false,
			err:  errors.New(""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := IsHTTPURL(tt.in)
			if want, wantErr := tt.want, tt.err; !(got == want) || ((err == nil && wantErr != nil) || (err != nil && wantErr == nil)) {
				t.Errorf("%s: got :%v, wanted output: %v, got error: %v, wanted error: %v", tt.desc, got, want, err, wantErr)
			}
		})
	}
}
