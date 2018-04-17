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

	buf.WriteString(name) // nolint: gas
	for _, k := range keys {
		buf.WriteString(";") // nolint: gas
		buf.WriteString(k)   // nolint: gas
		buf.WriteString("=") // nolint: gas

		switch v := labels[k].(type) {
		case string:
			buf.WriteString(v) // nolint: gas
		case int64:
			var bytes [32]byte
			buf.Write(strconv.AppendInt(bytes[:], v, 16)) // nolint: gas
		case float64:
			var bytes [32]byte
			buf.Write(strconv.AppendFloat(bytes[:], v, 'b', -1, 64)) // nolint: gas
		case bool:
			var bytes [32]byte
			buf.Write(strconv.AppendBool(bytes[:], v)) // nolint: gas
		case []byte:
			buf.Write(v) // nolint: gas
		case map[string]string:
			ws := keyWorkspacePool.Get().(*keyWorkspace)
			mk := ws.keys

			// ensure stable order
			for k2 := range v {
				mk = append(mk, k2)
			}
			sort.Strings(mk)

			for _, k2 := range mk {
				buf.WriteString(k2)    // nolint: gas
				buf.WriteString(v[k2]) // nolint: gas
			}

			ws.keys = keys[:0]
			keyWorkspacePool.Put(ws)
		default:
			buf.WriteString(v.(fmt.Stringer).String()) // nolint: gas
		}
	}

	result := buf.String()
	pool.PutBuffer(buf)

	ws.keys = keys[:0]
	keyWorkspacePool.Put(ws)

	return result
}
