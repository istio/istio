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

package controller

import "go.uber.org/atomic"

var SyncAllKinds = map[string]struct{}{
	"Services":      {},
	"Nodes":         {},
	"Pods":          {},
	"Endpoints":     {},
	"EndpointSlice": {},
}

func isSyncAllKind(kind string) bool {
	if _, ok := SyncAllKinds[kind]; ok {
		return true
	}
	return false
}

func shouldEnqueue(kind string, beginSync *atomic.Bool) bool {
	if isSyncAllKind(kind) && !beginSync.Load() {
		return false
	}
	return true
}
