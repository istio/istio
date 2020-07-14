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

package envoy

import (
	"context"
	"crypto/sha256"

	"istio.io/pkg/log"
)

// Watcher triggers reloads on changes to the proxy config
type Watcher interface {
	// Run the watcher loop (blocking call)
	Run(context.Context)
}

type watcher struct {
	updates func(interface{})
}

// NewWatcher creates a new watcher instance from a proxy agent
func NewWatcher(updates func(interface{})) Watcher {
	return &watcher{
		updates: updates,
	}
}

func (w *watcher) Run(ctx context.Context) {
	// kick start the proxy with partial state (in case there are no notifications coming)
	w.SendConfig()

	<-ctx.Done()
	log.Info("Watcher has successfully terminated")
}

func (w *watcher) SendConfig() {
	h := sha256.New()
	w.updates(h.Sum(nil))
}
