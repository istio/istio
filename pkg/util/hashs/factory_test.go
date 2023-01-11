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

package hashs

import (
	"bytes"
	"testing"
)

func TestFactory(t *testing.T) {
	testCases := []struct {
		name                   string
		str                    string
		wantSum                []byte
		wantStr                string
		wantUint64             uint64
		wantLittleEndianUint64 uint64
	}{
		{
			name: "foo",
			str:  "foo",
			// note: Different hash implementation may get different hash value
			wantSum:    []byte{51, 191, 0, 168, 89, 196, 186, 63},
			wantStr:    "33bf00a859c4ba3f",
			wantUint64: 4592198659407396659,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			h := New()
			h.Write([]byte(tt.str))
			if gotSum := h.Sum(nil); !bytes.Equal(tt.wantSum, gotSum) {
				t.Errorf("wantSum %v, but got %v", tt.wantSum, gotSum)
			}
			if gotStr := h.ToString(); tt.wantStr != gotStr {
				t.Errorf("wantStr %v, but got %v", tt.wantStr, gotStr)
			}
			if gotUint64 := h.ToUint64(); tt.wantUint64 != gotUint64 {
				t.Errorf("wantUint64 %v, but got %v", tt.wantUint64, gotUint64)
			}
		})
	}
}
