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
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/model"
)

const (
	resync          = 5 * time.Millisecond
	notifyThreshold = resync * 10
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

	countMutex := sync.Mutex{}
	count := 0

	incrementCount := func() {
		countMutex.Lock()
		defer countMutex.Unlock()
		count++
	}
	getCountAndReset := func() int {
		countMutex.Lock()
		defer countMutex.Unlock()
		i := count
		count = 0
		return i
	}

	ctl := NewConsulMonitor(cl, resync)
	ctl.AppendInstanceHandler(func(instance *api.CatalogService, event model.Event) error {
		incrementCount()
		return nil
	})

	ctl.AppendServiceHandler(func(instances []*api.CatalogService, event model.Event) error {
		incrementCount()
		return nil
	})

	stop := make(chan struct{})
	go ctl.Start(stop)
	defer close(stop)

	time.Sleep(notifyThreshold)
	getCountAndReset()

	time.Sleep(notifyThreshold)
	if i := getCountAndReset(); i != 0 {
		t.Errorf("got %d notifications from controller, want %d", i, 0)
	}

	// re-ordering of service instances -> does not trigger update
	tmpReview := reviews[0]
	reviews[0] = reviews[len(reviews)-1]
	reviews[len(reviews)-1] = tmpReview

	time.Sleep(notifyThreshold)
	if i := getCountAndReset(); i != 0 {
		t.Errorf("got %d notifications from controller, want %d", i, 0)
	}

	// same service, new tag -> triggers instance update
	ts.Productpage[0].ServiceTags = append(ts.Productpage[0].ServiceTags, "new|tag")
	time.Sleep(notifyThreshold)
	if i := getCountAndReset(); i != 1 {
		t.Errorf("got %d notifications from controller, want %d", i, 2)
	}

	// delete a service instance -> trigger instance update
	ts.Reviews = reviews[0:1]
	time.Sleep(notifyThreshold)
	if i := getCountAndReset(); i != 1 {
		t.Errorf("got %d notifications from controller, want %d", i, 1)
	}

	// delete a service -> trigger service and instance update
	delete(ts.Services, "productpage")
	time.Sleep(notifyThreshold)
	if i := getCountAndReset(); i != 2 {
		t.Errorf("got %d notifications from controller, want %d", i, 2)
	}
}
