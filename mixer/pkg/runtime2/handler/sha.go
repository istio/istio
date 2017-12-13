// Copyright 2017 Istio Authors
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
	"fmt"
	"io"
	"sort"

	"github.com/gogo/protobuf/proto"
	"istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/log"
)

// computeSha for individual adapters
func computeSha(handler istio_mixer_v1_config.Handler, instances map[string]istio_mixer_v1_config.Instance) [sha1.Size]byte {
	buf := new(bytes.Buffer)

	encode(buf, handler.Adapter)
	encode(buf, handler.Params)

	// instances in alphabetical order
	// TODO add instance details only if the handler cares about it.
	insts := make([]string, 0, len(instances))
	for _, k := range instances {
		insts = append(insts, k.Name)
	}
	sort.Strings(insts)

	for _, iname := range insts {
		inst := instances[iname]
		encode(buf, inst.Template)
		encode(buf, inst.Params)
	}

	sha := sha1.Sum(buf.Bytes())
	buf.Reset()
	return sha
}

func encode(w io.Writer, v interface{}) {
	var b []byte
	var err error

	switch t := v.(type) {
	case string:
		b = []byte(t)
	case proto.Message:
		if b, err = proto.Marshal(t); err != nil {
			log.Warnf("Failed to marshall %v into a proto: %v", t, err)
		}
	}

	if b == nil {
		log.Warnf("Falling back to fmt.Fprintf()", v)
		b = []byte(fmt.Sprintf("%+v", v))
	}

	if _, err = w.Write(b); err != nil {
		log.Warnf("Failed to write %s to a buffer: %v", string(b), err)
	}
}
