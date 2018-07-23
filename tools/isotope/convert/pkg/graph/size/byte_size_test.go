// Copyright 2018 Istio Authors
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

package size

import (
	"testing"
)

func TestFromInt64(t *testing.T) {
	tests := []struct {
		input int64
		size  ByteSize
		err   error
	}{
		{0, 0, nil},
		{10, 10, nil},
		{1024, 1024, nil},
		{-1, 0, NegativeSizeError{-1}},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()

			size, err := FromInt64(test.input)
			if test.err != err {
				t.Errorf("expected %v; actual %v", test.err, err)
			}
			if test.size != size {
				t.Errorf("expected %v; actual %v", test.size, size)
			}
		})
	}
}

func TestFromString(t *testing.T) {
	tests := []struct {
		input string
		size  ByteSize
		err   error
	}{
		{"0", 0, nil},
		{"10k", 10240, nil},
		{"10kb", 10240, nil},
		{"10Kb", 10240, nil},
		{"10KB", 10240, nil},
		{"10KiB", 10240, nil},
		{"10 k", 10240, nil},
		{"10 kb", 10240, nil},
		{"100 Mb", 104857600, nil},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()

			size, err := FromString(test.input)
			if test.err != err {
				t.Errorf("expected %v; actual %v", test.err, err)
			}
			if test.size != size {
				t.Errorf("expected %v; actual %v", test.size, size)
			}
		})
	}
}
