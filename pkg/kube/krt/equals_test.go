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

package krt_test

import (
	"sync/atomic"
	"testing"

	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/assert"
)

type valEqual struct {
	count *atomic.Int64
}

func (v valEqual) Equals(other valEqual) bool {
	v.count.Add(1)
	return true
}

type ptrEqual struct {
	count *atomic.Int64
}

func (p *ptrEqual) Equals(other *ptrEqual) bool {
	p.count.Add(1)
	return true
}

func TestEqual(t *testing.T) {
	v := valEqual{count: &atomic.Int64{}}
	ptr := ptrEqual{count: &atomic.Int64{}}

	krt.Equal(v, v)
	assert.Equal(t, v.count.Load(), 1)
	krt.Equal(&ptr, &ptr)
	assert.Equal(t, ptr.count.Load(), 1)
	krt.Equal(&v, &v)
	assert.Equal(t, v.count.Load(), 2)
	krt.Equal(ptr, ptr)
	assert.Equal(t, ptr.count.Load(), 2)
}
