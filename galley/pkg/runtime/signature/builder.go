//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package signature

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
)

var builderPool = sync.Pool{New: func() interface{} { return &Builder{} }}

// Builder is the signature builder.
type Builder struct {
	buffer bytes.Buffer
}

// GetBuilder returns a new instance Builder.
func GetBuilder() *Builder {
	return builderPool.Get().(*Builder)
}

// EncodeProto encodes the proto in the signature.
func (b *Builder) EncodeProto(p proto.Message) {
	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{}
	str, err := m.MarshalToString(p)
	if err != nil {
		// TODO: Handle the error case
		panic(err)
	}
	b.buffer.WriteString(str)
}

// EncodeString encodes the string in the signature.
func (b *Builder) EncodeString(str string) {
	b.buffer.WriteString(str)
}

// Calculate the signature as a string.
func (b *Builder) Calculate() string {
	signature := sha256.Sum256(b.buffer.Bytes())

	dst := make([]byte, hex.EncodedLen(len(signature)))
	hex.Encode(dst, signature[:])

	return string(dst)
}

// Done returns the builder back to the pool.
func (b *Builder) Done() {
	builderPool.Put(b)
}
