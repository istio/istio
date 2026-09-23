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

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/proto/merge"
)

// isMergeOperation reports whether the operation merges the patch value into the
// existing config. Both MERGE and MERGE_AND_REPLACE_LIST are merge operations; they
// differ only in how repeated (list) fields are handled (see mergePatchValue).
func isMergeOperation(operation networking.EnvoyFilter_Patch_Operation) bool {
	return operation == networking.EnvoyFilter_Patch_MERGE ||
		operation == networking.EnvoyFilter_Patch_MERGE_AND_REPLACE_LIST
}

// mergePatchValue merges src into dst using the semantics of the given patch operation.
// MERGE appends repeated (list) fields, while MERGE_AND_REPLACE_LIST replaces them
// wholesale. Both operations merge scalar and message fields identically.
//
// Note: this only governs the top-level proto merge. Merges of Any-typed configs
// (HTTP/network/listener filters and transport sockets) go through mergeAnyPatchValue,
// which applies the same semantics to the message carried by the Any.
func mergePatchValue(operation networking.EnvoyFilter_Patch_Operation, dst, src proto.Message) {
	if operation == networking.EnvoyFilter_Patch_MERGE_AND_REPLACE_LIST {
		merge.MergeWithReplaceList(dst, src)
		return
	}
	merge.Merge(dst, src)
}

// mergeAnyPatchValue merges the src Any into the dst Any using the semantics of the given
// patch operation, dynamically inferring the concrete type carried by the Any. It is the
// Any-typed counterpart of mergePatchValue: MERGE appends repeated (list) fields of the
// carried message, while MERGE_AND_REPLACE_LIST replaces them wholesale.
//
// This is what allows lists nested inside an Any to be overridden, e.g. replacing the
// alpn_protocols of a transport socket or the list of rules of an HTTP filter, instead of
// appending the patched values to the ones Istio already generated.
func mergeAnyPatchValue(operation networking.EnvoyFilter_Patch_Operation, dst, src *anypb.Any) (*anypb.Any, error) {
	if operation == networking.EnvoyFilter_Patch_MERGE_AND_REPLACE_LIST {
		return util.MergeAnyWithAnyReplaceList(dst, src)
	}
	return util.MergeAnyWithAny(dst, src)
}

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
