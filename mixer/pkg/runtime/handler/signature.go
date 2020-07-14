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

package handler

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"io"
	"reflect"
	"sort"

	"github.com/gogo/protobuf/proto"

	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

type signature [sha1.Size]byte

// Use a zero signature as a sentinel.
var zeroSignature = signature{}

func (s signature) equals(other signature) bool {
	// equality to zeroSignature always returns false. This ensures that we don't accidentally create
	// signatures with missing configuration data due to serialization errors.
	return !bytes.Equal(s[:], zeroSignature[:]) && bytes.Equal(s[:], other[:])
}

// calculateSignature returns signature given handler and array of dynamic or static instances
func calculateSignature(handler hndlr, insts interface{}) signature {
	if reflect.TypeOf(insts).Kind() != reflect.Slice {
		return zeroSignature
	}

	instances := reflect.ValueOf(insts)

	// sort the instances by name
	instanceMap := make(map[string]inst)
	instanceNames := make([]string, instances.Len())
	for i := 0; i < instances.Len(); i++ {
		ii := instances.Index(i).Interface()
		instance := ii.(inst)
		instanceMap[instance.GetName()] = instance
		instanceNames[i] = instance.GetName()
	}
	sort.Strings(instanceNames)

	buf := pool.GetBuffer()
	encoded := true

	encoded = encoded && encode(buf, handler.AdapterName())
	if handler.AdapterParams() != nil &&
		(reflect.ValueOf(handler.AdapterParams()).Kind() != reflect.Ptr || !reflect.ValueOf(handler.AdapterParams()).IsNil()) {
		encoded = encoded && encode(buf, handler.AdapterParams())
	}
	if handler.ConnectionConfig() != nil {
		encoded = encoded && encode(buf, handler.ConnectionConfig())
	}

	for _, name := range instanceNames {
		instance := instanceMap[name]
		encoded = encoded && encode(buf, instance.TemplateName())
		encoded = encoded && encode(buf, instance.TemplateParams())
	}

	if encoded {
		sha := sha1.Sum(buf.Bytes())
		pool.PutBuffer(buf)
		return sha
	}

	pool.PutBuffer(buf)
	return zeroSignature
}

type hndlr interface {
	GetName() string
	AdapterName() string
	AdapterParams() interface{}
	ConnectionConfig() interface{}
}
type inst interface {
	GetName() string
	TemplateName() string
	TemplateParams() interface{}
}

func encode(w io.Writer, v interface{}) bool {
	var b []byte
	var err error

	switch t := v.(type) {
	case string:
		b = []byte(t)
	case proto.Marshaler:
		// TODO (Issue #2539): This is likely to yield poor results, as proto serialization is not guaranteed
		// to be stable, especially when maps are involved... We should probably have a better model here.
		if b, err = t.Marshal(); err != nil {
			log.Warnf("Failed to marshal %v into a proto: %v", t, err)
			b = nil
		}
	default:
		if b, err = json.Marshal(t); err != nil {
			log.Warnf("Failed to json marshal %v into a proto: %v", t, err)
			b = nil
		}
	}

	if b == nil {
		return false
	}

	_, err = w.Write(b)
	return err == nil
}
