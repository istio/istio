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

package il

import (
	"testing"
)

func TestIntegerRoundtrip(t *testing.T) {
	d := []int64{
		0,
		-1,
		1,
		10,
		1 << 33,
		1 << 34,
		-1 << 35,
	}

	for _, v := range d {
		o1, o2 := IntegerToByteCode(v)
		a := ByteCodeToInteger(o1, o2)
		if a != v {
			t.Fatalf("Conversion mismatch: E:%v, A:%v", v, a)
		}
	}
}

func TestIDoubleRoundtrip(t *testing.T) {
	d := []float64{
		0,
		0.1,
		-0.1,
		-1.23,
		1,
		213123123, 322323,
		1 << 33,
		1 << 34,
		-1 << 35,
	}

	for _, v := range d {
		o1, o2 := DoubleToByteCode(v)
		a := ByteCodeToDouble(o1, o2)
		if a != v {
			t.Fatalf("Conversion mismatch: E:%v, A:%v", v, a)
		}
	}
}

func TestIBoolRoundtrip(t *testing.T) {
	d := []bool{
		false,
		true,
	}

	for _, v := range d {
		o := BoolToByteCode(v)
		a := ByteCodeToBool(o)
		if a != v {
			t.Fatalf("Conversion mismatch: E:%v, A:%v", v, a)
		}
	}
}
