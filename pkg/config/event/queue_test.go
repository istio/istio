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

package event

import (
	"fmt"
	"strconv"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/resource"
)

func TestQueue_Empty(t *testing.T) {
	g := NewGomegaWithT(t)

	q := &queue{}

	g.Expect(q.isEmpty()).To(BeTrue())
	g.Expect(q.isFull()).To(BeTrue())

	_, ok := q.pop()
	g.Expect(ok).To(BeFalse())
}

func TestQueueWrapEmpty(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 100; i++ {
		a := wrap(i, 0)
		g.Expect(a).To(Equal(i))
	}
}

func TestQueue_Expand_And_Use(t *testing.T) {

	q := &queue{}

	addCtr := 0
	popCtr := 0
	for max := 1; max < 513; max++ {
		t.Run(fmt.Sprintf("M%d", max), func(t *testing.T) {
			g := NewGomegaWithT(t)

			g.Expect(q.isEmpty()).To(BeTrue())
			g.Expect(q.size()).To(Equal(0))

			for i := 0; i < max; i++ {
				e := genEvent(&addCtr)
				q.add(e)

				g.Expect(q.size()).To(Equal(i + 1))
			}

			if max == len(q.items)-1 {
				g.Expect(q.isFull()).To(BeTrue())
			} else {
				g.Expect(q.isFull()).To(BeFalse())
			}

			for i := 0; i < max; i++ {
				a, ok := q.pop()
				g.Expect(ok).To(BeTrue())

				g.Expect(matchesEventSequence(&popCtr, a)).To(BeTrue())
			}

			g.Expect(q.isEmpty()).To(BeTrue())
			g.Expect(q.isFull()).To(BeFalse())
			g.Expect(q.size()).To(Equal(0))

			_, ok := q.pop()
			g.Expect(ok).To(BeFalse())
		})
	}
}

func genEvent(ctr *int) Event {
	vStr := fmt.Sprintf("%d", *ctr)
	*ctr++

	return Event{
		Kind: Added,
		Resource: &resource.Instance{
			Metadata: resource.Metadata{
				Version: resource.Version(vStr),
			},
		},
	}
}

func matchesEventSequence(ctr *int, e Event) bool {
	*ctr++
	i, err := strconv.ParseInt(string(e.Resource.Metadata.Version), 10, 64)
	if err != nil {
		return false
	}

	return int(i) == *ctr-1
}
