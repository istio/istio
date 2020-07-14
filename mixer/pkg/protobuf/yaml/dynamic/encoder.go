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

package dynamic

import (
	"fmt"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/attribute"
)

type (
	// Encoder transforms yaml/json that represents protobuf data into []byte
	// String values (rvalues) are treated as expressions that are evaluated
	// using attribute bag at runtime.
	// Quoted String values are treated as literals.
	Encoder interface {
		Encode(bag attribute.Bag, ba []byte) ([]byte, error)
	}

	messageEncoder struct {
		// skipEncodeLength skip encoding length of the message in the output
		// should be true only for top level message.
		skipEncodeLength bool

		// fields of the message.
		fields []*fieldEncoder
	}

	fieldEncoder struct {
		// proto key  -- EncodeVarInt ((field_number << 3) | wire_type)
		protoKey []byte
		// if packed this is set to true
		packed bool

		// packed fields have a list of encoders.
		// non-packed fields have one.
		encoder []Encoder

		// number fields are sorted by fieldEncoder number.
		number int
		// name for debug.
		name string
	}
)

// extendSlice add small amount of data to a byte array.
func extendSlice(ba []byte, n int) []byte {
	for k := 0; k < n; k++ {
		ba = append(ba, 0x0)
	}
	return ba
}

func (m messageEncoder) encodeWithoutLength(bag attribute.Bag, ba []byte) ([]byte, error) {
	var err error
	for _, f := range m.fields {
		ba, err = f.Encode(bag, ba)
		if err != nil {
			return nil, fmt.Errorf("fieldEncoder: %s - %v", f.name, err)
		}
	}
	return ba, nil
}

// expected length of the varint encoded word
// 2 byte words represent 2 ** 14 = 16K bytes
// 1 byte words represent 2 ** 7 = 128 bytes
// If message length is more, it involves an array copy
const defaultMsgLengthSize = 2

var msgLengthSize = defaultMsgLengthSize

// encode message including length of the message into []byte
func (m messageEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	var err error

	if m.skipEncodeLength {
		return m.encodeWithoutLength(bag, ba)
	}

	prefixIdx := len(ba)
	// #pragma inline reserve fieldLengthSize bytes
	ba = extendSlice(ba, msgLengthSize)
	l1 := len(ba)

	if ba, err = m.encodeWithoutLength(bag, ba); err != nil {
		return nil, err
	}

	length := len(ba) - l1
	diff := proto.SizeVarint(uint64(length)) - msgLengthSize
	// move data forward because we need more than fieldLengthSize bytes
	if diff > 0 {
		ba = extendSlice(ba, diff)
		// shift data down. This should rarely occur.
		copy(ba[l1+diff:], ba[l1:])
	}

	// ignore return value. EncodeLength is writing in the middle of the array.
	_ = EncodeVarintZeroExtend(ba[prefixIdx:prefixIdx], uint64(length), msgLengthSize)

	return ba, nil
}

func (m messageEncoder) EncodesLength() bool {
	return !m.skipEncodeLength
}

// expected length of the varint encoded word
// 2 byte words represent 2 ** 14 = 16K bytes
// If the repeated fieldEncoder length is more, it involves an array copy
const defaultFieldLengthSize = 1

var fieldLengthSize = defaultFieldLengthSize

func (f fieldEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	ba = append(ba, f.protoKey...)

	var prefixIdx int
	var startOfDataIdx int

	if f.packed {
		prefixIdx = len(ba)
		// #pragma inline reserve fieldLengthSize bytes
		ba = extendSlice(ba, fieldLengthSize)
		startOfDataIdx = len(ba)
	}

	var err error
	for _, en := range f.encoder {
		ba, err = en.Encode(bag, ba)
		if err != nil {
			return nil, err
		}
	}

	if f.packed {
		length := len(ba) - startOfDataIdx
		diff := proto.SizeVarint(uint64(length)) - fieldLengthSize
		// move data forward because we need more than fieldLengthSize bytes
		if diff > 0 {
			ba = extendSlice(ba, diff)
			// shift data down. This should rarely occur.
			copy(ba[startOfDataIdx+diff:], ba[startOfDataIdx:])
		}

		// ignore return value. EncodeLength is writing in the middle of the array.
		_ = EncodeVarintZeroExtend(ba[prefixIdx:prefixIdx], uint64(length), fieldLengthSize)
	}

	return ba, nil
}
