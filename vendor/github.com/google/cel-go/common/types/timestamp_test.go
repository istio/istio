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
	"testing"

	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types/ref"

	dpb "github.com/golang/protobuf/ptypes/duration"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
)

func TestTimestamp_Add(t *testing.T) {
	ts := Timestamp{&tpb.Timestamp{Seconds: 7506}}
	val := ts.Add(Duration{&dpb.Duration{Seconds: 3600, Nanos: 1000}})
	if val.ConvertToType(TypeType) != TimestampType {
		t.Error("Could not add duration and timestamp")
	}
	expected := Timestamp{&tpb.Timestamp{Seconds: 11106, Nanos: 1000}}
	if !expected.Compare(val).Equal(IntZero).(Bool) {
		t.Errorf("Got '%v', expected '%v'", val, expected)
	}
	if !IsError(ts.Add(expected)) {
		t.Error("Cannot add two timestamps together")
	}
}

func TestTimestamp_Subtract(t *testing.T) {
	ts := Timestamp{&tpb.Timestamp{Seconds: 7506}}
	val := ts.Subtract(Duration{&dpb.Duration{Seconds: 3600, Nanos: 1000}})
	if val.ConvertToType(TypeType) != TimestampType {
		t.Error("Could not add duration and timestamp")
	}
	expected := Timestamp{&tpb.Timestamp{Seconds: 3905, Nanos: 999999000}}
	if !expected.Compare(val).Equal(IntZero).(Bool) {
		t.Errorf("Got '%v', expected '%v'", val, expected)
	}
}

func TestTimestamp_ReceiveGetHours(t *testing.T) {
	// 1970-01-01T02:05:05Z
	ts := Timestamp{&tpb.Timestamp{Seconds: 7506}}
	hr := ts.Receive(overloads.TimeGetHours, overloads.TimestampToHours, []ref.Value{})
	if !hr.Equal(Int(2)).(Bool) {
		t.Error("Expected 2 hours, got", hr)
	}
	// 1969-12-31T19:05:05Z
	hrTz := ts.Receive(overloads.TimeGetHours, overloads.TimestampToHoursWithTz,
		[]ref.Value{String("America/Phoenix")})
	if !hrTz.Equal(Int(19)).(Bool) {
		t.Error("Expected 19 hours, got", hrTz)
	}
}

func TestTimestamp_ReceiveGetMinutes(t *testing.T) {
	// 1970-01-01T02:05:05Z
	ts := Timestamp{&tpb.Timestamp{Seconds: 7506}}
	min := ts.Receive(overloads.TimeGetMinutes, overloads.TimestampToMinutes, []ref.Value{})
	if !min.Equal(Int(5)).(Bool) {
		t.Error("Expected 5 minutes, got", min)
	}
	// 1969-12-31T19:05:05Z
	minTz := ts.Receive(overloads.TimeGetMinutes, overloads.TimestampToMinutesWithTz,
		[]ref.Value{String("America/Phoenix")})
	if !minTz.Equal(Int(5)).(Bool) {
		t.Error("Expected 5 minutes, got", minTz)
	}
}

func TestTimestamp_ReceiveGetSeconds(t *testing.T) {
	// 1970-01-01T02:05:05Z
	ts := Timestamp{&tpb.Timestamp{Seconds: 7506}}
	sec := ts.Receive(overloads.TimeGetSeconds, overloads.TimestampToSeconds, []ref.Value{})
	if !sec.Equal(Int(6)).(Bool) {
		t.Error("Expected 6 seconds, got", sec)
	}
	// 1969-12-31T19:05:05Z
	secTz := ts.Receive(overloads.TimeGetSeconds, overloads.TimestampToSecondsWithTz,
		[]ref.Value{String("America/Phoenix")})
	if !secTz.Equal(Int(6)).(Bool) {
		t.Error("Expected 6 seconds, got", secTz)
	}
}
