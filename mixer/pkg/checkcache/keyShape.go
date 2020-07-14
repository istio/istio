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

package checkcache

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/pkg/attribute"
	"istio.io/pkg/pool"
)

// A reference to an attribute and to an optional map key.
type attributeRef = attribute.Reference

// keyShape describes the attributes that must/must not participate in key formation
type keyShape struct {
	// These attributes must be absent from the input bag
	absentAttrs []attributeRef

	// These attributes must be present in the input bag
	presentAttrs []attributeRef
}

const (
	delimiter = 0
)

func newKeyShape(ra mixerpb.ReferencedAttributes, globalWords []string) keyShape {
	ks := keyShape{}

	for _, match := range ra.AttributeMatches {
		var ar attributeRef

		ar.Name = getString(match.Name, globalWords, ra.Words)
		ar.MapKey = getString(match.MapKey, globalWords, ra.Words)

		if match.Condition == mixerpb.ABSENCE {
			ks.absentAttrs = append(ks.absentAttrs, ar)
		} else if match.Condition == mixerpb.EXACT {
			ks.presentAttrs = append(ks.presentAttrs, ar)
		}
	}

	sort.Slice(ks.absentAttrs, func(i int, j int) bool {
		return ks.absentAttrs[i].Name < ks.absentAttrs[j].Name
	})

	sort.Slice(ks.presentAttrs, func(i int, j int) bool {
		return ks.presentAttrs[i].Name < ks.presentAttrs[j].Name
	})

	return ks
}

// isCompatible determines whether the input bag meets the requirements to be used
// with this instance
func (ks keyShape) isCompatible(attrs attribute.Bag) bool {
	return ks.checkAbsentAttrs(attrs) && ks.checkPresentAttrs(attrs)
}

// checkAbsentAttrs ensures that the input bag doesn't include any of the attributes that must be absent
func (ks keyShape) checkAbsentAttrs(attrs attribute.Bag) bool {
	for i := 0; i < len(ks.absentAttrs); i++ {
		name := ks.absentAttrs[i].Name

		v, ok := attrs.Get(name)
		if !ok {
			// it's absent, all good
			continue
		}

		sm, ok := v.(attribute.StringMap)
		if !ok {
			// attribute is present and it's not a string map, so this bag isn't compatible
			return false
		}

		// since absentAttrs is sorted by name, continue processing stringMaps until a new name is found.
		for {
			_, ok := sm.Get(ks.absentAttrs[i].MapKey)
			if ok {
				// if the map key is present, then this bag won't work
				return false
			}

			// break loop if at the end or name changes.
			if i+1 == len(ks.absentAttrs) || ks.absentAttrs[i+1].Name != name {
				break
			}
			i++
		}
	}

	// compatible
	return true
}

// checkPresentAttrs ensures that the input bag includes all of the attributes that must be present
func (ks keyShape) checkPresentAttrs(attrs attribute.Bag) bool {
	for i := 0; i < len(ks.presentAttrs); i++ {
		name := ks.presentAttrs[i].Name

		v, ok := attrs.Get(name)
		if !ok {
			// attribute not present, not compatible
			return false
		}

		sm, ok := v.(attribute.StringMap)
		if !ok {
			// not a string map, so we're good
			continue
		}

		// since presentAttrs is sorted by name, continue processing stringMaps until a new name is found.
		for {
			_, ok := sm.Get(ks.presentAttrs[i].MapKey)
			if !ok {
				// string map key is missing, not compatible
				return false
			}

			// break loop if at the end or name changes.
			if i+1 == len(ks.presentAttrs) || ks.presentAttrs[i+1].Name != name {
				break
			}
			i++
		}
	}

	// compatible
	return true
}

// makeKey creates a cache key for the given attribute bag.
// This function assumes that the bag has previously been deemed compatible via isCompatible.
func (ks keyShape) makeKey(attrs attribute.Bag) string {
	buf := pool.GetBuffer()
	b := make([]byte, 8)

	for i := 0; i < len(ks.presentAttrs); i++ {
		name := ks.presentAttrs[i].Name

		buf.WriteString(name)
		buf.WriteByte(delimiter)

		v, _ := attrs.Get(name) // no need to check for errors, since isCompatible already checked this

		// refer to attribute.Bag for the supported attribute value types
		switch v := v.(type) {
		case string:
			buf.WriteString(v)

		case []byte:
			buf.Write(v)

		case time.Duration:
		case int64:
			binary.LittleEndian.PutUint64(b, uint64(v))
			buf.Write(b)

		case float64:
			binary.LittleEndian.PutUint64(b, math.Float64bits(v))
			buf.Write(b)

		case bool:
			if v {
				buf.WriteByte(1)
			} else {
				buf.WriteByte(0)
			}

		case time.Time:
			binary.LittleEndian.PutUint64(b, uint64(v.UnixNano()))
			buf.Write(b)

		case attribute.StringMap:
			// Since presentAttrs is sorted by name, continue processing stringMaps until a new name is found.
			for {
				v2, _ := v.Get(ks.presentAttrs[i].MapKey)

				buf.WriteString(ks.presentAttrs[i].MapKey)
				buf.WriteByte(delimiter)
				buf.WriteString(v2)
				buf.WriteByte(delimiter)

				// break loop if at the end or name changes.
				if i+1 == len(ks.presentAttrs) || ks.presentAttrs[i+1].Name != name {
					break
				}
				i++
			}

		default:
			panic(fmt.Errorf("unexpected type %T", v))
		}

		buf.WriteByte(delimiter)
	}

	/* According to benchmarks, this version of the code speeds up lookups by about 1/3. Alas, it
	can lead to pretty long keys, which can use a lot of memory. We'll need to try this out in a more
	realistic environment and understand the exact memory cost for this approach.

	It's possible to cut down the memory used by this approach slightly by using the global dictionary to encode
	attribute names instead of raw strings. This adds 1/13th more overhead per lookup.

	result := buf.String()
	pool.PutBuffer(buf)
	return result
	*/

	hasher := md5.New()
	// do not expect an error here
	_, _ = buf.WriteTo(hasher)
	pool.PutBuffer(buf)
	result := hasher.Sum(nil)

	return string(result)
}

// Gets a string from the local or global word list, based on the supplied index value.
func getString(index int32, globalWords []string, localWords []string) string {
	if index >= 0 {
		if index >= int32(len(globalWords)) {
			return ""
		}
		return globalWords[index]
	}

	index = -index - 1
	if index >= int32(len(localWords)) {
		return ""
	}

	return localWords[index]
}
