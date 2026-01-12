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

package envoyfilter

// replaceFunc find and replace the first matching element.
// If the f function returns true, the returned value will be used for replacement.
func replaceFunc[E any](s []E, f func(e E) (bool, E)) ([]E, bool) {
	applied := false
	for k, v := range s {
		if ok, r := f(v); ok {
			s[k] = r
			applied = true
			break
		}
	}
	return s, applied
}

// insertBeforeFunc find and insert an element before the found element.
// If the f function returns true, the returned value will be inserted before the found element.
func insertBeforeFunc[E any](s []E, f func(e E) (bool, E)) ([]E, bool) {
	var toInsert E
	idx := -1
	for k, v := range s {
		if ok, r := f(v); ok {
			toInsert = r
			idx = k
			break
		}
	}

	if idx == -1 {
		return s, false
	}

	s = append(s, toInsert) // for grow the cap
	copy(s[idx+1:], s[idx:])
	s[idx] = toInsert

	return s, true
}

// insertAfterFunc find and insert an element after the found element.
// If the f function returns true, the returned value will be inserted after the found element.
func insertAfterFunc[E any](s []E, f func(e E) (bool, E)) ([]E, bool) {
	var toInsert E
	idx := -1
	for k, v := range s {
		if ok, r := f(v); ok {
			toInsert = r
			idx = k
			break
		}
	}

	if idx == -1 {
		return s, false
	}

	// insert after the last element
	if idx == len(s) {
		s = append(s, toInsert)
		return s, true
	}

	// insert after not the last element
	// this equals insert before idx+1
	idx++
	s = append(s, toInsert) // for grow the cap
	copy(s[idx+1:], s[idx:])
	s[idx] = toInsert

	return s, true
}
