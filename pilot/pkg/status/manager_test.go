/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package status

import (
	"testing"

	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	"istio.io/api/meta/v1alpha1"
	network "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/test/util/assert"
)

var createResource = func(name string) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             name,
			Namespace:        name,
			Generation:       10,
		},
		Spec: &network.VirtualService{
			Http: []*network.HTTPRoute{
				{
					Match: []*network.HTTPMatchRequest{
						{
							Name: "match",
							Port: 80,
						},
					},
					Route: []*network.HTTPRouteDestination{
						{
							Destination: &network.Destination{
								Host: "test",
							},
							Weight: 100,
						},
					},
				},
			},
		},
	}
}

var (
	testResource   = createResource("test")
	randomResource = createResource("random")
)

func getResource(name string) Resource {
	return Resource{
		GroupVersionResource: gvr.VirtualService,
		Namespace:            name,
		Name:                 name,
		Generation:           "10",
	}
}

func TestManagerUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	// random resource is to make sure test resource is wrote before its processing
	store := memory.Make(collection.SchemasFor(collections.Istio.All()...))
	_, err := store.Create(testResource)
	assert.NoError(t, err)
	_, err = store.Create(randomResource)
	assert.NoError(t, err)
	manager := NewManager(store)
	x := make(chan struct{})

	fakefunc := func(status *v1alpha1.IstioStatus, context any) *v1alpha1.IstioStatus {
		x <- struct{}{}
		return context.(*v1alpha1.IstioStatus)
	}
	controller := manager.CreateIstioStatusController(fakefunc)

	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})

	is, ok := manager.lastStatuses.Load(testResource.Key())
	assert.Equal(t, ok, false)
	assert.Equal(t, is, nil)

	manager.workers.Push(getResource("test"), controller, &v1alpha1.IstioStatus{
		Conditions:         nil,
		ValidationMessages: nil,
		ObservedGeneration: 10,
	})
	<-x

	manager.workers.Push(getResource("random"), controller, &v1alpha1.IstioStatus{})
	<-x

	last, ok := manager.lastStatuses.Load(testResource.Key())
	g.Expect(ok).To(Equal(true))
	g.Expect(proto.Equal(last.(*v1alpha1.IstioStatus), &v1alpha1.IstioStatus{
		Conditions:         nil,
		ValidationMessages: nil,
		ObservedGeneration: 10,
	})).To(Equal(true))

	// update with the same status
	manager.workers.Push(getResource("test"), controller, &v1alpha1.IstioStatus{
		Conditions:         nil,
		ValidationMessages: nil,
		ObservedGeneration: 10,
	})
	<-x

	manager.workers.Push(getResource("random"), controller, &v1alpha1.IstioStatus{})
	<-x

	last, ok = manager.lastStatuses.Load(testResource.Key())
	g.Expect(ok).To(Equal(true))
	g.Expect(proto.Equal(last.(*v1alpha1.IstioStatus), &v1alpha1.IstioStatus{
		Conditions:         nil,
		ValidationMessages: nil,
		ObservedGeneration: 10,
	})).To(Equal(true))

	// update with different status
	manager.workers.Push(getResource("test"), controller, &v1alpha1.IstioStatus{
		Conditions: []*v1alpha1.IstioCondition{
			{
				Type:    "newstatus",
				Reason:  "test",
				Message: "test",
			},
		},
		ValidationMessages: nil,
		ObservedGeneration: 10,
	})
	<-x

	manager.workers.Push(getResource("random"), controller, &v1alpha1.IstioStatus{})
	<-x

	last, ok = manager.lastStatuses.Load(testResource.Key())
	g.Expect(ok).To(Equal(true))
	g.Expect(proto.Equal(last.(*v1alpha1.IstioStatus), &v1alpha1.IstioStatus{
		Conditions: []*v1alpha1.IstioCondition{
			{
				Type:    "newstatus",
				Reason:  "test",
				Message: "test",
			},
		},
		ValidationMessages: nil,
		ObservedGeneration: 10,
	})).To(Equal(true))
}
