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

package memquota

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"istio.io/istio/mixer/pkg/pool"
)

// we maintain a pool of these for use by the makeKey function
type keyWorkspace struct {
	keys []string
}

// pool of reusable keyWorkspace structs
var keyWorkspacePool = sync.Pool{New: func() interface{} { return &keyWorkspace{} }}

// makeKey produces a unique key representing the given labels.
func makeKey(name string, labels map[string]interface{}) string {
	ws := keyWorkspacePool.Get().(*keyWorkspace)
	keys := ws.keys
	buf := pool.GetBuffer()

	// ensure stable order
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteString(name)
	for _, k := range keys {
		buf.WriteString(";")
		buf.WriteString(k)
		buf.WriteString("=")

		switch v := labels[k].(type) {
		case string:
			buf.WriteString(v)
		case int64:
			var bytes [32]byte
			buf.Write(strconv.AppendInt(bytes[:], v, 16))
		case float64:
			var bytes [32]byte
			buf.Write(strconv.AppendFloat(bytes[:], v, 'b', -1, 64))
		case bool:
			var bytes [32]byte
			buf.Write(strconv.AppendBool(bytes[:], v))
		case []byte:
			buf.Write(v)
		case map[string]string:
			ws := keyWorkspacePool.Get().(*keyWorkspace)
			mk := ws.keys

			// ensure stable order
			for k2 := range v {
				mk = append(mk, k2)
			}
			sort.Strings(mk)

			for _, k2 := range mk {
				buf.WriteString(k2)
				buf.WriteString(v[k2])
			}

			ws.keys = keys[:0]
			keyWorkspacePool.Put(ws)
		default:
			buf.WriteString(v.(fmt.Stringer).String())
		}
	}

	result := buf.String()
	pool.PutBuffer(buf)

	ws.keys = keys[:0]
	keyWorkspacePool.Put(ws)

	return result
}
