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

package diag

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/resource"
)

func TestMessages_Sort(t *testing.T) {
	g := NewGomegaWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "A1", "Template: %q"),
		testResource("B"),
		"B",
	)
	thirdMsg := NewMessage(
		NewMessageType(Warning, "B1", "Template: %q"),
		testResource("A"),
		"B",
	)
	fourthMsg := NewMessage(
		NewMessageType(Warning, "B1", "Template: %q"),
		testResource("B"),
		"A",
	)
	fifthMsg := NewMessage(
		NewMessageType(Warning, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)

	msgs := Messages{fifthMsg, fourthMsg, thirdMsg, secondMsg, firstMsg}
	expectedMsgs := Messages{firstMsg, secondMsg, thirdMsg, fourthMsg, fifthMsg}

	msgs.Sort()

	g.Expect(msgs).To(Equal(expectedMsgs))
}

func TestMessages_SortWithNilOrigin(t *testing.T) {
	g := NewGomegaWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		nil,
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		nil,
		"C",
	)
	thirdMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)

	msgs := Messages{thirdMsg, secondMsg, firstMsg}
	expectedMsgs := Messages{firstMsg, secondMsg, thirdMsg}

	msgs.Sort()

	g.Expect(msgs).To(Equal(expectedMsgs))
}

func TestMessages_SortedCopy(t *testing.T) {
	g := NewGomegaWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "A1", "Template: %q"),
		testResource("B"),
		"B",
	)
	// Oops, we have a duplicate (identical to firstMsg) - it should be removed.
	thirdMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)

	msgs := Messages{thirdMsg, secondMsg, firstMsg}
	expectedMsgs := Messages{firstMsg, secondMsg}

	newMsgs := msgs.SortedDedupedCopy()

	g.Expect(newMsgs).To(Equal(expectedMsgs))
}

func testResource(name string) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewShortOrFullName("default", name),
		},
		Origin: testOrigin{name: name},
	}
}
