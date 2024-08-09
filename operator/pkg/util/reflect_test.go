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
	"reflect"
	"testing"
)

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// to ptr conversion utility functions
func toInt8Ptr(i int8) *int8 { return &i }

func TestIsValueNil(t *testing.T) {
	if !IsValueNil(nil) {
		t.Error("got IsValueNil(nil) false, want true")
	}
	if !IsValueNil((*int)(nil)) {
		t.Error("got IsValueNil(ptr) false, want true")
	}
	if !IsValueNil(map[int]int(nil)) {
		t.Error("got IsValueNil(map) false, want true")
	}
	if !IsValueNil([]int(nil)) {
		t.Error("got IsValueNil(slice) false, want true")
	}
	if !IsValueNil(any(nil)) {
		t.Error("got IsValueNil(interface) false, want true")
	}

	if IsValueNil(toInt8Ptr(42)) {
		t.Error("got IsValueNil(ptr) true, want false")
	}
	if IsValueNil(map[int]int{42: 42}) {
		t.Error("got IsValueNil(map) true, want false")
	}
	if IsValueNil([]int{1, 2, 3}) {
		t.Error("got IsValueNil(slice) true, want false")
	}
	if IsValueNil(any(42)) {
		t.Error("got IsValueNil(interface) true, want false")
	}
}

func TestDeleteFromSlicePtr(t *testing.T) {
	parentSlice := []int{42, 43, 44, 45}
	var parentSliceI any = parentSlice
	if err := DeleteFromSlicePtr(&parentSliceI, 1); err != nil {
		t.Fatalf("got error: %s, want error: nil", err)
	}
	wantSlice := []int{42, 44, 45}
	if got, want := parentSliceI, wantSlice; !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%v\nwant:\n%v\n", got, want)
	}

	badParent := struct{}{}
	wantErr := `deleteFromSlicePtr parent type is *struct {}, must be *[]interface{}`
	if got, want := errToString(DeleteFromSlicePtr(&badParent, 1)), wantErr; got != want {
		t.Fatalf("got error: %s, want error: %s", got, want)
	}
}

func TestInsertIntoMap(t *testing.T) {
	parentMap := map[int]string{42: "forty two", 43: "forty three"}
	key := 44
	value := "forty four"
	if err := InsertIntoMap(parentMap, key, value); err != nil {
		t.Fatalf("got error: %s, want error: nil", err)
	}
	wantMap := map[int]string{42: "forty two", 43: "forty three", 44: "forty four"}
	if got, want := parentMap, wantMap; !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%v\nwant:\n%v\n", got, want)
	}

	badParent := struct{}{}
	wantErr := `insertIntoMap parent type is *struct {}, must be map`
	if got, want := errToString(InsertIntoMap(&badParent, key, value)), wantErr; got != want {
		t.Fatalf("got error: %s, want error: %s", got, want)
	}
}

var (
	allIntTypes     = []any{int(-42), int8(-43), int16(-44), int32(-45), int64(-46)}
	allUintTypes    = []any{uint(42), uint8(43), uint16(44), uint32(45), uint64(46)}
	allIntegerTypes = append(allIntTypes, allUintTypes...)
	nonIntTypes     = []any{nil, "", []int{}, map[string]bool{}}
	allTypes        = append(allIntegerTypes, nonIntTypes...)
)

func TestIsInteger(t *testing.T) {
	tests := []struct {
		desc     string
		function func(v reflect.Kind) bool
		want     []any
	}{
		{
			desc:     "ints",
			function: IsIntKind,
			want:     allIntTypes,
		},
		{
			desc:     "uints",
			function: IsUintKind,
			want:     allUintTypes,
		},
	}

	for _, tt := range tests {
		var got []any
		for _, v := range allTypes {
			if tt.function(reflect.ValueOf(v).Kind()) {
				got = append(got, v)
			}
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.desc, got, tt.want)
		}
	}
}
