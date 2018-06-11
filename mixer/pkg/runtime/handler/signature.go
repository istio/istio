// Copyright 2018 Istio Authors
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
	"io"
	"sort"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/pkg/log"
	"encoding/json"
	"istio.io/istio/mixer/pkg/adapter"
)

type signature [sha1.Size]byte

// Use a zero signature as a sentinel.
var zeroSignature = signature{}

func (s signature) equals(other signature) bool {
	// equality to zeroSignature always returns false. This ensures that we don't accidentally create
	// signatures with missing configuration data due to serialization errors.
	return !bytes.Equal(s[:], zeroSignature[:]) && bytes.Equal(s[:], other[:])
}

func calculateSignatureDynamic(handler *adapter.DynamicHandler, instances []*adapter.DynamicInstance) signature {

	// sort the instances by name
	instanceMap := make(map[string]*adapter.DynamicInstance)
	instanceNames := make([]string, len(instances))
	for i, instance := range instances {
		instanceMap[instance.Name] = instance
		instanceNames[i] = instance.Name
	}
	sort.Strings(instanceNames)

	buf := new(bytes.Buffer)
	encoded := true

	encoded = encoded && encode(buf, handler.Adapter.Name)
	encoded = encoded && encode(buf, handler.AdapterConfig)
	for _, name := range instanceNames {
		instance := instanceMap[name]
		encoded = encoded && encode(buf, instance.Template.Name)
		encoded = encoded && encode(buf, instance.Params)
	}

	if encoded {
		sha := sha1.Sum(buf.Bytes())
		buf.Reset()
		return sha
	}

	return zeroSignature
}

func calculateSignature(handler *config.HandlerStatic, instances []*config.InstanceStatic) signature {

	// sort the instances by name
	instanceMap := make(map[string]*config.InstanceStatic)
	instanceNames := make([]string, len(instances))
	for i, instance := range instances {
		instanceMap[instance.Name] = instance
		instanceNames[i] = instance.Name
	}
	sort.Strings(instanceNames)

	buf := new(bytes.Buffer)
	encoded := true

	encoded = encoded && encode(buf, handler.Adapter.Name)
	encoded = encoded && encode(buf, handler.Params)
	for _, name := range instanceNames {
		instance := instanceMap[name]
		encoded = encoded && encode(buf, instance.Template.Name)
		encoded = encoded && encode(buf, instance.Params)
	}

	if encoded {
		sha := sha1.Sum(buf.Bytes())
		buf.Reset()
		return sha
	}

	return zeroSignature
}

func encode(w io.Writer, v interface{}) bool {
	var b []byte
	var err error

	switch t := v.(type) {
	case string:
		b = []byte(t)
	case proto.Message:
		// TODO (Issue #2539): This is likely to yield poor results, as proto serialization is not guaranteed
		// to be stable, especially when maps are involved... We should probably have a better model here.
		if b, err = proto.Marshal(t); err != nil {
			log.Warnf("Failed to marshal proto %T %v: %v", t, t, err)
			b = nil
		}
	case []byte:
		b = t
	default: // attempt json marshaling
		// for map[string]interface{} the encoding *is* stable
		if b, err = json.Marshal(t); err != nil {
			log.Warnf("Failed to marshal as json %T %v: %v", t, t, err)
			b = nil
		}
	}

	if b == nil {
		return false
	}

	_, err = w.Write(b)
	return err == nil
}
