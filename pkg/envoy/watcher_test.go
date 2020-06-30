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
	"testing"
	"time"
)

type TestAgent struct {
	configCh chan interface{}
}

func (ta *TestAgent) Restart(c interface{}) {
	ta.configCh <- c
}

func (ta *TestAgent) Run(ctx context.Context) {
	<-ctx.Done()
}

func TestRunSendConfig(t *testing.T) {
	agent := &TestAgent{
		configCh: make(chan interface{}),
	}
	watcher := NewWatcher(agent.Restart)
	ctx, cancel := context.WithCancel(context.Background())

	// watcher starts agent and schedules a config update
	go watcher.Run(ctx)

	select {
	case <-agent.configCh:
		// expected
		cancel()
	case <-time.After(time.Second):
		t.Errorf("The callback is not called within time limit " + time.Now().String())
		cancel()
	}
}
