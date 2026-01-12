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

package merge

import (
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/istio/pkg/test/util/assert"
)

func TestMerge(t *testing.T) {
	src := &durationpb.Duration{Seconds: 123, Nanos: 456}
	dst := &durationpb.Duration{Seconds: 789, Nanos: 999}

	srcListener := &listener.Listener{ListenerFiltersTimeout: src}
	dstListener := &listener.Listener{ListenerFiltersTimeout: dst}

	Merge(dstListener, srcListener)

	assert.Equal(t, dstListener.ListenerFiltersTimeout, src)

	// dst duration not changed after merge
	assert.Equal(t, dst, &durationpb.Duration{Seconds: 789, Nanos: 999})
}
