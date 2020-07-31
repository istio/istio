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
	g := NewWithT(t)

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
	g := NewWithT(t)

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
	g := NewWithT(t)

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

func TestMessages_SetRefDoc(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Info, "C1", "Template: %q"),
		testResource("B"),
		"B",
	)

	msgs := Messages{firstMsg, secondMsg}
	msgs.SetDocRef("istioctl-awesome")

	getDocURL := func(msg Message) string {
		return msg.Unstructured(false)["documentation_url"].(string)
	}

	g.Expect(getDocURL(msgs[0])).To(Equal("https://istio.io/docs/reference/config/analysis/b1/?ref=istioctl-awesome"))
	g.Expect(getDocURL(msgs[1])).To(Equal("https://istio.io/docs/reference/config/analysis/c1/?ref=istioctl-awesome"))
}

func TestMessages_Filter(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Template: %q"),
		testResource("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Info, "A1", "Template: %q"),
		testResource("B"),
		"B",
	)
	thirdMsg := NewMessage(
		NewMessageType(Warning, "C1", "Template: %q"),
		testResource("B"),
		"B",
	)

	msgs := Messages{firstMsg, secondMsg, thirdMsg}
	filteredMsgs := msgs.Filter(Warning)
	expectedMsgs := Messages{firstMsg, thirdMsg}

	g.Expect(filteredMsgs).To(Equal(expectedMsgs))
}

func TestMessages_FilterOutAll(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Info, "A1", "Template: %q"),
		testResource("B"),
		"B",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Template: %q"),
		testResource("B"),
		"B",
	)

	msgs := Messages{firstMsg, secondMsg}
	filteredMsgs := msgs.Filter(Error)
	expectedMsgs := Messages{}

	g.Expect(filteredMsgs).To(Equal(expectedMsgs))
}

func TestMessages_PrintLog(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Explosion accident: %v"),
		testResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Collapse danger: %v"),
		testResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := Messages{firstMsg, secondMsg}
	output, _ := msgs.Print(LogFormat, false)

	g.Expect(output).To(Equal(
		"Error [B1] (SoapBubble) Explosion accident: the bubble is too big\n" +
			"Warn [C1] (GrandCastle) Collapse danger: the castle is too old",
	))
}

func TestMessages_PrintJSON(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Explosion accident: %v"),
		testResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Collapse danger: %v"),
		testResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := Messages{firstMsg, secondMsg}
	output, _ := msgs.Print(JSONFormat, false)

	expectedOutput := `[
	{
		"code": "B1",
		"documentation_url": "https://istio.io/docs/reference/config/analysis/b1/",
		"level": "Error",
		"message": "Explosion accident: the bubble is too big",
		"origin": "SoapBubble"
	},
	{
		"code": "C1",
		"documentation_url": "https://istio.io/docs/reference/config/analysis/c1/",
		"level": "Warn",
		"message": "Collapse danger: the castle is too old",
		"origin": "GrandCastle"
	}
]`

	g.Expect(output).To(Equal(expectedOutput))
}

func TestMessages_PrintYAML(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Explosion accident: %v"),
		testResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Collapse danger: %v"),
		testResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := Messages{firstMsg, secondMsg}
	output, _ := msgs.Print(YAMLFormat, false)

	expectedOutput := `- code: B1
  documentation_url: https://istio.io/docs/reference/config/analysis/b1/
  level: Error
  message: 'Explosion accident: the bubble is too big'
  origin: SoapBubble
- code: C1
  documentation_url: https://istio.io/docs/reference/config/analysis/c1/
  level: Warn
  message: 'Collapse danger: the castle is too old'
  origin: GrandCastle
`

	g.Expect(output).To(Equal(expectedOutput))
}

func TestMessages_PrintEmpty(t *testing.T) {
	g := NewWithT(t)

	msgs := Messages{}

	logOutput, _ := msgs.PrintLog(false)
	g.Expect(logOutput).To(Equal(""))

	jsonOutput, _ := msgs.PrintJSON()
	g.Expect(jsonOutput).To(Equal("[]"))

	yamlOutput, _ := msgs.PrintYAML()
	g.Expect(yamlOutput).To(Equal("[]\n"))
}

func testResource(name string) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewShortOrFullName("default", name),
		},
		Origin: testOrigin{name: name},
	}
}
