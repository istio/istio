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

package yaml

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
)

var namedEncoderRegistry = make(map[string]namedEncoderFn)

func registerNE(msgname string, encode encodeFunc) {
	name := "." + msgname
	ne := &namedEncoder{name: name, enc: encode}
	namedEncoderRegistry[name] = ne.encode
}

type encodeFunc func(name string, v string) ([]byte, error)
type namedEncoder struct {
	name string
	enc  encodeFunc
}

func (n namedEncoder) encode(data interface{}) ([]byte, error) {
	v, ok := data.(string)
	if !ok {
		return nil, unexpectedTypeError(n.name, "string", data)
	}
	return n.enc(n.name, v)
}

func init() {

	registerNE(proto.MessageName((*types.Duration)(nil)), func(name string, v string) ([]byte, error) {
		var d time.Duration
		var err error
		if d, err = time.ParseDuration(v); err != nil {
			return nil, err
		}
		return types.DurationProto(d).Marshal()
	})

	registerNE(proto.MessageName((*types.Timestamp)(nil)), func(name string, v string) ([]byte, error) {
		var d time.Time
		var err error
		if d, err = time.Parse(time.RFC3339, v); err != nil {
			return nil, err
		}
		var ts *types.Timestamp
		if ts, err = types.TimestampProto(d); err != nil {
			return nil, err
		}
		return ts.Marshal()
	})
}

func unexpectedTypeError(msgName, wantType string, data interface{}) error {
	return fmt.Errorf("message type '%s'  got:type '%T' want '%s'", msgName, data, wantType)
}
