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

package sync

import (
	"sync"

	"google.golang.org/protobuf/proto"
)

type CopyOnWrite[T proto.Message] struct {
	value    T
	copyOnce sync.Once
}

func NewCopyOnWrite[T proto.Message](value T) *CopyOnWrite[T] {
	return &CopyOnWrite[T]{value: value}
}

func (cow *CopyOnWrite[T]) Value() T {
	return cow.value
}

func (cow *CopyOnWrite[T]) Mutable() T {
	cow.copyOnce.Do(func() {
		cow.value = proto.Clone(cow.value).(T)
	})
	return cow.value
}
