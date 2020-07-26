// Copyright Istio Authors.
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

package yaml

import (
	"reflect"
	"testing"
)

type tdata struct {
	n       string
	in      interface{}
	expData interface{}
	expOK   bool
}

func TestFloat(t *testing.T) {
	for _, td := range []tdata{
		{
			n:       "validFloat32",
			in:      float32(123.0),
			expOK:   true,
			expData: float64(123.0),
		},
		{
			n:       "validFloat64",
			in:      float64(123.0),
			expOK:   true,
			expData: float64(123.0),
		},
		{
			n:       "validInt",
			in:      int(123),
			expOK:   true,
			expData: float64(123),
		},
		{
			n:       "bad",
			in:      "",
			expOK:   false,
			expData: float64(0),
		},
	} {
		t.Run(td.n, func(tt *testing.T) {
			got, gotOk := ToFloat(td.in)
			if !reflect.DeepEqual(got, td.expData) || td.expOK != gotOk {
				t.Errorf("input '%T(%v)' got '%T(%v)', %t; want '%T(%v)', %t", td.in, td.in, got, got, gotOk,
					td.expData, td.expData, td.expOK)
			}
		})
	}
}

func TestInt(t *testing.T) {
	for _, td := range []tdata{
		{
			n:       "validint",
			in:      int(123),
			expOK:   true,
			expData: int64(123),
		},
		{
			n:       "validint8",
			in:      int8(123),
			expOK:   true,
			expData: int64(123),
		},
		{
			n:       "validint16",
			in:      int16(123),
			expOK:   true,
			expData: int64(123),
		},
		{
			n:       "validint32",
			in:      int32(123),
			expOK:   true,
			expData: int64(123),
		},
		{
			n:       "validint64",
			in:      int64(123),
			expOK:   true,
			expData: int64(123),
		},
		{
			n:       "bad",
			in:      "",
			expOK:   false,
			expData: int64(0),
		},
	} {
		t.Run(td.n, func(tt *testing.T) {
			got, gotOk := ToInt64(td.in)
			if !reflect.DeepEqual(got, td.expData) || td.expOK != gotOk {
				t.Errorf("input '%T(%v)' got '%T(%v)', %t; want '%T(%v)', %t", td.in, td.in, got, got, gotOk,
					td.expData, td.expData, td.expOK)
			}
		})
	}
}
