// Copyright 2017 Google Inc.
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

package metrics

import "testing"

func TestValue_Bool(t *testing.T) {
	v := Value{metricValue: true}

	got, err := v.Bool()
	if err != nil {
		t.Errorf("Bool() => unexpected error: %v (for value: %v)", err, v)
	}
	if got != v.metricValue {
		t.Errorf("Bool() => %t, wanted %t", got, v.metricValue)
	}
}

func TestValue_BoolError(t *testing.T) {
	v := Value{metricValue: "test"}

	if _, err := v.Bool(); err == nil {
		t.Errorf("Bool() expected error for value of type %T", v.metricValue)
	}
}

func TestValue_String(t *testing.T) {
	v := Value{metricValue: "test"}

	got, err := v.String()
	if err != nil {
		t.Errorf("String() => unexpected error: %v (for value: %v)", err, v)
	}
	if got != v.metricValue {
		t.Errorf("String() => %s, wanted %s", got, v.metricValue)
	}
}

func TestValue_StringError(t *testing.T) {
	v := Value{metricValue: 32}

	if _, err := v.String(); err == nil {
		t.Errorf("String() expected error for value of type %T", v.metricValue)
	}
}

func TestValue_Int64(t *testing.T) {
	v := Value{metricValue: int64(79)}

	got, err := v.Int64()
	if err != nil {
		t.Errorf("Int64() => unexpected error: %v (for value: %v)", err, v)
	}
	if got != v.metricValue {
		t.Errorf("Int64() => %d, wanted %d", got, v.metricValue)
	}
}

func TestValue_Int64Error(t *testing.T) {
	v := Value{metricValue: "test"}

	if _, err := v.Int64(); err == nil {
		t.Errorf("Int64() expected error for value of type %T", v.metricValue)
	}
}

func TestValue_Float64(t *testing.T) {
	v := Value{metricValue: 64.3454}

	got, err := v.Float64()
	if err != nil {
		t.Errorf("Float64() => unexpected error: %v (for value: %v)", err, v)
	}
	if got != v.metricValue {
		t.Errorf("Float64() => %f, wanted %f", got, v.metricValue)
	}
}

func TestValue_Float64Error(t *testing.T) {
	v := Value{metricValue: "test"}

	if _, err := v.Float64(); err == nil {
		t.Errorf("Float64() expected error for value of type %T", v.metricValue)
	}
}
