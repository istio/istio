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

package validation

import (
	"encoding/binary"
	"unsafe"
)

var nativeByteOrder binary.ByteOrder

func init() {
	var x uint16 = 0x0102
	lowerByte := *(*byte)(unsafe.Pointer(&x))
	switch lowerByte {
	case 0x01:
		nativeByteOrder = binary.BigEndian
	case 0x02:
		nativeByteOrder = binary.LittleEndian
	default:
		panic("Could not determine native byte order.")
	}
}

// <arpa/inet.h>
func ntohs(n16 uint16) uint16 {
	if nativeByteOrder == binary.BigEndian {
		return n16
	}
	return (n16&0xff00)>>8 | (n16&0xff)<<8
}
