// Copyright 2017 Istio Authors
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

const (
	resync          = 5 * time.Millisecond
	notifyThreshold = resync * 100
)

func TestController(t *testing.T) {
	ts := newServer()
	defer ts.Server.Close()
	conf := api.DefaultConfig()
	conf.Address = ts.Server.URL

	cl, err := api.NewClient(conf)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	updateChannel := make(chan struct{}, 10)

	ctl := NewConsulMonitor(cl, resync)
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

	drain := func(c chan struct{}) int {
		found := 0
		for {
			select {
			case <-c:
				found++
			case <-time.After(notifyThreshold):
				return found
			}
		}
	}
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
		if left := drain(updateChannel); left != 0 {
			t.Fatalf("got %d notifications, want %d", times+left, times)
		}
	}

	// Ignore initial updates
	drain(updateChannel)

	expectNotify(t, 0)

	// re-ordering of service instances -> does not trigger update
	ts.Lock.Lock()
	reviews[0], reviews[len(reviews)-1] = reviews[len(reviews)-1], reviews[0]
	ts.Lock.Unlock()
	expectNotify(t, 0)

	// same service, new tag -> triggers instance update
	ts.Lock.Lock()
	ts.Productpage[0].ServiceTags = append(ts.Productpage[0].ServiceTags, "new|tag")
	ts.Lock.Unlock()
	expectNotify(t, 1)

	// delete a service instance -> trigger instance update
	ts.Lock.Lock()
	ts.Reviews = reviews[0:1]
	ts.Lock.Unlock()
	expectNotify(t, 1)

	// delete a service -> trigger service and instance update
	ts.Lock.Lock()
	delete(ts.Services, "productpage")
	ts.Lock.Unlock()
	expectNotify(t, 2)
}
