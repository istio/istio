// Copyright 2019 Istio Authors
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
)

func TestMessages_Sort(t *testing.T) {
	g := NewGomegaWithT(t)
	var msgs Messages
	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testOrigin("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "A1", "Template: %q"),
		testOrigin("B"),
		"B",
	)
	thirdMsg := NewMessage(
		NewMessageType(Warning, "B1", "Template: %q"),
		testOrigin("A"),
		"B",
	)
	fourthMsg := NewMessage(
		NewMessageType(Warning, "B1", "Template: %q"),
		testOrigin("B"),
		"A",
	)
	fifthMsg := NewMessage(
		NewMessageType(Warning, "B1", "Template: %q"),
		testOrigin("B"),
		"B",
	)
	msgs.Add(fifthMsg)
	msgs.Add(fourthMsg)
	msgs.Add(thirdMsg)
	msgs.Add(secondMsg)
	msgs.Add(firstMsg)

	g.Expect(msgs[0]).To(Equal(fifthMsg))

	msgs.Sort()

	g.Expect(msgs[0]).To(Equal(firstMsg))
	g.Expect(msgs[1]).To(Equal(secondMsg))
	g.Expect(msgs[2]).To(Equal(thirdMsg))
	g.Expect(msgs[3]).To(Equal(fourthMsg))
	g.Expect(msgs[4]).To(Equal(fifthMsg))
}

func TestMessages_Sorted(t *testing.T) {
	g := NewGomegaWithT(t)
	var msgs Messages
	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testOrigin("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "A1", "Template: %q"),
		testOrigin("B"),
		"B",
	)
	msgs.Add(secondMsg)
	msgs.Add(firstMsg)

	newMsgs := msgs.Sorted()

	g.Expect(msgs[0]).To(Equal(secondMsg))
	g.Expect(msgs[1]).To(Equal(firstMsg))

	g.Expect(newMsgs[0]).To(Equal(firstMsg))
	g.Expect(newMsgs[1]).To(Equal(secondMsg))
}
