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

package consul

import (
	"testing"
	"time"

	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/pkg/model"
)

const notifyThreshold = 10 * time.Second

func TestController(t *testing.T) {
	ts := newServer()
	defer ts.server.Close()
	conf := api.DefaultConfig()
	conf.Address = ts.server.URL

	cl, err := api.NewClient(conf)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	updateChannel := make(chan struct{}, 10)

	ctl := NewConsulMonitor(cl)
	ctl.AppendInstanceHandler(func(instance *api.CatalogService, event model.Event) error {
		updateChannel <- struct{}{}
		return nil
	})

	ctl.AppendServiceHandler(func(instances []*api.CatalogService, event model.Event) error {
		updateChannel <- struct{}{}
		return nil
	})

	stop := make(chan struct{})
	go ctl.Start(stop)
	defer close(stop)

	expectNotify := func(t *testing.T, times int) {
		t.Helper()
		for i := 0; i < times; i++ {
			select {
			case <-updateChannel:
				continue
			case <-time.After(notifyThreshold):
				t.Fatalf("got %d notifications from controller, want %d", i, times)
			}
		}
	}

	//The first query from monitor to Consul always doesn't block because the index is 0
	expectNotify(t, 2)

	//There won't be any notifications if X-Consul-Index doesn't change
	expectNotify(t, 0)

	//X-Consul-Index change means that the Consul Catalog changes, so there will be notifications
	ts.lock.Lock()
	ts.consulIndex++
	ts.lock.Unlock()
	expectNotify(t, 2)
}
