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
package config

import (
	"errors"
	"strings"
	"testing"
)

func TestNewStore(t *testing.T) {
	for _, tt := range []struct {
		url string
		err error
	}{
		{"fs:///tmp", nil},
		{"redis://:passwd@localhost:6379/1", errors.New("getsockopt")}, // connection error to the server
		{"etcd:///tmp/testdata/configroot", errors.New("unknown")},
		{"/tmp/testdata/configroot", errors.New("unknown")},
	} {
		t.Run(tt.url, func(t *testing.T) {
			_, err := NewStore(tt.url)
			if err == tt.err {
				return
			}

			if err != nil {
				if tt.err == nil || !strings.Contains(err.Error(), tt.err.Error()) {
					t.Errorf("got %s\nwant %s", err, tt.err)
				}
			}
		})
	}
}
