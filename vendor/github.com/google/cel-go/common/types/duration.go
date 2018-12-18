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
	"fmt"
	"reflect"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	dpb "github.com/golang/protobuf/ptypes/duration"
)

// Duration type that implements ref.Value and supports add, compare, negate,
// and subtract operators. This type is also a receiver which means it can
// participate in dispatch to receiver functions.
type Duration struct {
	*dpb.Duration
}

var (
	// DurationType singleton.
	DurationType = NewTypeValue("google.protobuf.Duration",
		traits.AdderType,
		traits.ComparerType,
		traits.NegatorType,
		traits.ReceiverType,
		traits.SubtractorType)
)

// Add implements traits.Adder.Add.
func (d Duration) Add(other ref.Value) ref.Value {
	switch other.Type() {
	case DurationType:
		dur1, err := ptypes.Duration(d.Duration)
		if err != nil {
			return &Err{err}
		}
		dur2, err := ptypes.Duration(other.(Duration).Duration)
		if err != nil {
			return &Err{err}
		}
		return Duration{ptypes.DurationProto(dur1 + dur2)}
	case TimestampType:
		dur, err := ptypes.Duration(d.Duration)
		if err != nil {
			return &Err{err}
		}
		ts, err := ptypes.Timestamp(other.(Timestamp).Timestamp)
		if err != nil {
			return &Err{err}
		}
		tstamp, err := ptypes.TimestampProto(ts.Add(dur))
		if err != nil {
			return &Err{err}
		}
		return Timestamp{tstamp}
	}
	return NewErr("unsupported overload")
}

// Compare implements traits.Comparer.Compare.
func (d Duration) Compare(other ref.Value) ref.Value {
	if DurationType != other.Type() {
		return NewErr("unsupported overload")
	}
	dur1, err := ptypes.Duration(d.Duration)
	if err != nil {
		return &Err{err}
	}
	dur2, err := ptypes.Duration(other.(Duration).Duration)
	if err != nil {
		return &Err{err}
	}
	dur := dur1 - dur2
	if dur < 0 {
		return IntNegOne
	}
	if dur > 0 {
		return IntOne
	}
	return IntZero
}

// ConvertToNative implements ref.Value.ConvertToNative.
func (d Duration) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	if typeDesc == durationValueType {
		return d.Value(), nil
	}
	// If the duration is already assignable to the desired type return it.
	if reflect.TypeOf(d).AssignableTo(typeDesc) {
		return d, nil
	}
	return nil, fmt.Errorf("type conversion error from "+
		"'google.protobuf.Duration' to '%v'", typeDesc)
}

// ConvertToType implements ref.Value.ConvertToType.
func (d Duration) ConvertToType(typeVal ref.Type) ref.Value {
	switch typeVal {
	case StringType:
		if dur, err := ptypes.Duration(d.Duration); err == nil {
			return String(dur.String())
		}
	case IntType:
		if dur, err := ptypes.Duration(d.Duration); err == nil {
			return Int(dur)
		}
	case DurationType:
		return d
	case TypeType:
		return DurationType
	}
	return NewErr("type conversion error from '%s' to '%s'", DurationType, typeVal)
}

// Equal implements ref.Value.Equal.
func (d Duration) Equal(other ref.Value) ref.Value {
	return Bool(DurationType == other.Type() &&
		proto.Equal(d.Duration, other.Value().(proto.Message)))
}

// Negate implements traits.Negater.Negate.
func (d Duration) Negate() ref.Value {
	dur, err := ptypes.Duration(d.Duration)
	if err != nil {
		return &Err{err}
	}
	return Duration{ptypes.DurationProto(-dur)}
}

// Receive implements traits.Receiver.Receive.
func (d Duration) Receive(function string, overload string, args []ref.Value) ref.Value {
	dur, err := ptypes.Duration(d.Duration)
	if err != nil {
		return &Err{err}
	}
	if len(args) == 0 {
		if f, found := durationZeroArgOverloads[function]; found {
			return f(dur)
		}
	}
	return NewErr("unsupported overload")
}

// Subtract implements traits.Subtractor.Subtract.
func (d Duration) Subtract(subtrahend ref.Value) ref.Value {
	if DurationType != subtrahend.Type() {
		return NewErr("unsupported overload")
	}
	return d.Add(subtrahend.(Duration).Negate())
}

// Type implements ref.Value.Type.
func (d Duration) Type() ref.Type {
	return DurationType
}

// Value implements ref.Value.Value.
func (d Duration) Value() interface{} {
	return d.Duration
}

var (
	durationValueType = reflect.TypeOf(&dpb.Duration{})

	durationZeroArgOverloads = map[string]func(time.Duration) ref.Value{
		overloads.TimeGetHours: func(dur time.Duration) ref.Value {
			return Int(dur.Hours())
		},
		overloads.TimeGetMinutes: func(dur time.Duration) ref.Value {
			return Int(dur.Minutes())
		},
		overloads.TimeGetSeconds: func(dur time.Duration) ref.Value {
			return Int(dur.Seconds())
		},
		overloads.TimeGetMilliseconds: func(dur time.Duration) ref.Value {
			return Int(dur.Nanoseconds() / 1000000)
		}}
)
