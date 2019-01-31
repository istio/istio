// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types/ref"

	dpb "github.com/golang/protobuf/ptypes/duration"
)

func TestDuration_Add(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506}}
	if !d.Add(d).Equal(Duration{&dpb.Duration{Seconds: 15012}}).(Bool) {
		t.Error("Adding duration and itself did not double it.")
	}
}

func TestDuration_Compare(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506}}
	lt := Duration{&dpb.Duration{Seconds: -10}}
	if d.Compare(lt).(Int) != IntOne {
		t.Error("Larger duration was not considered greater than smaller one.")
	}
	if lt.Compare(d).(Int) != IntNegOne {
		t.Error("Smaller duration was not less than larger one.")
	}
	if d.Compare(d).(Int) != IntZero {
		t.Error("Durations were not considered equal.")
	}
	if !IsError(d.Compare(False)) {
		t.Error("Got comparison result, expected error.")
	}
}

func TestDuration_ConvertToNative(t *testing.T) {
	val, err := Duration{&dpb.Duration{Seconds: 7506, Nanos: 1000}}.
		ConvertToNative(reflect.TypeOf(&dpb.Duration{}))
	if err != nil ||
		!proto.Equal(val.(proto.Message), &dpb.Duration{Seconds: 7506, Nanos: 1000}) {
		t.Errorf("Got '%v', expected backing proto message value", err)
	}
}

func TestDuration_ConvertToNative_Error(t *testing.T) {
	val, err := Duration{&dpb.Duration{Seconds: 7506, Nanos: 1000}}.
		ConvertToNative(jsonValueType)
	if err == nil {
		t.Errorf("Got '%v', expected error", val)
	}
}

func TestDuration_ConvertToType_Identity(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506, Nanos: 1000}}
	str := d.ConvertToType(StringType).(String)
	if str != "2h5m6.000001s" {
		t.Errorf("Got '%v', wanted 2h5m6.000001s", str)
	}
	i := d.ConvertToType(IntType).(Int)
	if i != Int(7506000001000) {
		t.Errorf("Got '%v', wanted 7506000001000", i)
	}
	if !d.ConvertToType(DurationType).Equal(d).(Bool) {
		t.Errorf("Got '%v', wanted identity", d.ConvertToType(DurationType))
	}
	if d.ConvertToType(TypeType) != DurationType {
		t.Errorf("Got '%v', expected duration type", d.ConvertToType(TypeType))
	}
	if !IsError(d.ConvertToType(UintType)) {
		t.Errorf("Got value, expected error.")
	}
}

func TestDuration_Negate(t *testing.T) {
	neg := Duration{&dpb.Duration{Seconds: 1234, Nanos: 1}}.Negate().(Duration)
	if !proto.Equal(neg.Duration, &dpb.Duration{Seconds: -1234, Nanos: -1}) {
		t.Errorf("Got '%v', expected seconds: -1234, nanos: -1", neg)
	}
}

func TestDuration_Receive_GetHours(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506}}
	hr := d.Receive(overloads.TimeGetHours, overloads.DurationToHours, []ref.Value{})
	if !hr.Equal(Int(2)).(Bool) {
		t.Error("Expected 2 hours, got", hr)
	}
}

func TestDuration_Receive_GetMinutes(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506}}
	min := d.Receive(overloads.TimeGetMinutes, overloads.DurationToMinutes, []ref.Value{})
	if !min.Equal(Int(125)).(Bool) {
		t.Error("Expected 5 minutes, got", min)
	}
}

func TestDuration_Receive_GetSeconds(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506}}
	sec := d.Receive(overloads.TimeGetSeconds, overloads.DurationToSeconds, []ref.Value{})
	if !sec.Equal(Int(7506)).(Bool) {
		t.Error("Expected 6 seconds, got", sec)
	}
}

func TestDuration_Subtract(t *testing.T) {
	d := Duration{&dpb.Duration{Seconds: 7506}}
	if !d.Subtract(d).ConvertToType(IntType).Equal(IntZero).(Bool) {
		t.Error("Subtracting a duration from itself did not equal zero.")
	}
}
