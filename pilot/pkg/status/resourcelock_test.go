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

package status

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"testing"
	"time"
	. "github.com/onsi/gomega"
)

func TestResourceLock_Lock(t *testing.T) {
	g := NewGomegaWithT(t)
	r1 := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group: "r1",
			Version: "r1",
		},
		Namespace:            "r1",
		Name:                 "r1",
		ResourceVersion:      "r1.1",
	}
	r1a := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group: "r1",
			Version: "r1",
		},
		Namespace:            "r1",
		Name:                 "r1",
		ResourceVersion:      "r1.2",
	}
	rlock := ResourceMutex{}
	callAssertions := map[string]bool{}
	callActual := map[string]bool{}
	printer := func(nth string, expect bool) func(_ context.Context, config Resource, _ Progress){
		if expect {
			callAssertions[nth] = expect
		}
		return func(_ context.Context, config Resource, _ Progress){
			if !expect {
				t.Errorf("did not expect %s call to occur", nth)
			}
			callActual[nth] = true
			fmt.Printf("starting %s %s\n", nth, config.ResourceVersion)
			time.Sleep(1*time.Second)
			fmt.Printf("finishing %s %s\n", nth, config.ResourceVersion)
		}
	}
	rlock.OncePerResource(nil, r1, Progress{}, printer("first", true))
	time.Sleep(10*time.Millisecond)
	rlock.OncePerResource(nil, r1a, Progress{}, printer("second", true))
	time.Sleep(10*time.Millisecond)
	r1.ResourceVersion = "r1.3"
	for i := 0; i <1000; i++ {
		rlock.OncePerResource(nil, r1, Progress{}, printer("third", false) )
	}
	time.Sleep(5*time.Second)
	rlock.OncePerResource(nil, r1, Progress{}, printer("fourth", true))
	time.Sleep(1100*time.Millisecond)

	rlock.OncePerResource(nil, r1, Progress{}, printer("fifth", true))
	time.Sleep(10*time.Millisecond)
	rlock.OncePerResource(nil, r1a, Progress{}, printer("sixth", false))
	rlock.OncePerResource(nil, r1a, Progress{}, printer("seventh", false))
	rlock.Delete(r1a)
	time.Sleep(2200*time.Millisecond)
	g.Expect(callAssertions).To(Equal(callActual))
}
