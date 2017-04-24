// Copyright 2017 The Istio Authors.
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

package adapter

import (
	"testing"
	"time"
)

func TestString(t *testing.T) {
	val := Value{MetricValue: 1}
	if _, err := val.String(); err == nil {
		t.Error("val.String() = _, nil; wanted err")
	}
	mv := "foo"
	val.MetricValue = mv
	s, err := val.String()
	if err != nil {
		t.Errorf("val.String() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.String() = %s; wanted '%v'", s, mv)
	}
}

func TestBool(t *testing.T) {
	val := Value{MetricValue: "foo"}
	if _, err := val.Bool(); err == nil {
		t.Error("val.Bool() = _, nil; wanted err")
	}
	mv := true
	val.MetricValue = mv
	s, err := val.Bool()
	if err != nil {
		t.Errorf("val.Bool() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Bool() = %t; wanted '%v'", s, mv)
	}
}

func TestInt64(t *testing.T) {
	val := Value{MetricValue: false}
	if _, err := val.Int64(); err == nil {
		t.Error("val.Int64() = _, nil; wanted err")
	}
	mv := int64(1)
	val.MetricValue = mv
	s, err := val.Int64()
	if err != nil {
		t.Errorf("val.Int64() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Int64() = %d; wanted '%v'", s, mv)
	}
}

func TestFloat64(t *testing.T) {
	val := Value{MetricValue: int64(1)}
	if _, err := val.Float64(); err == nil {
		t.Error("val.Float64() = _, nil; wanted err")
	}
	mv := 37.0
	val.MetricValue = mv
	s, err := val.Float64()
	if err != nil {
		t.Errorf("val.Float64() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Float64() = %f; wanted '%v'", s, mv)
	}
}

func TestDuration(t *testing.T) {
	val := Value{MetricValue: 345}
	if _, err := val.Duration(); err == nil {
		t.Error("val.Duration() = _, nil; wanted err")
	}
	mv := 45254 * time.Microsecond
	val.MetricValue = mv
	s, err := val.Duration()
	if err != nil {
		t.Errorf("val.Duration() = _, %v; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Duration() = %v; wanted '%v'", s, mv)
	}
}
