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

package activenotifier

import (
	"sync"

	"go.uber.org/atomic"
)

type ActiveNotifier struct {
	state        *atomic.Bool
	handlers     []func(state bool)
	handlerMutex sync.Mutex
}

// CurrentlyActive returns whether the state is currently active.
func (n *ActiveNotifier) CurrentlyActive() bool {
	return n.state.Load()
}

// AddHandler adds a handler. It will be called on any update, including immediately with the current state
func (n *ActiveNotifier) AddHandler(f func(bool)) {
	n.handlerMutex.Lock()
	defer n.handlerMutex.Unlock()
	n.handlers = append(n.handlers, f)
	f(n.state.Load())
}

// StoreAndNotify stores the current state and notifies all handlers
func (n *ActiveNotifier) StoreAndNotify(b bool) {
	n.handlerMutex.Lock()
	defer n.handlerMutex.Unlock()
	n.state.Store(b)
	for _, h := range n.handlers {
		h(b)
	}
}

func New(initialState bool) *ActiveNotifier {
	return &ActiveNotifier{
		state:        atomic.NewBool(initialState),
		handlers:     nil,
		handlerMutex: sync.Mutex{},
	}
}
