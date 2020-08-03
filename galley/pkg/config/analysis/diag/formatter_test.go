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
)

func TestFormatter_PrintLog(t *testing.T) {
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
	output, _ := Print(msgs, LogFormat, false)

	g.Expect(output).To(Equal(
		"Error [B1] (SoapBubble) Explosion accident: the bubble is too big\n" +
			"Warn [C1] (GrandCastle) Collapse danger: the castle is too old",
	))
}

func TestFormatter_PrintJSON(t *testing.T) {
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
	output, _ := Print(msgs, JSONFormat, false)

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

func TestFormatter_PrintYAML(t *testing.T) {
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
	output, _ := Print(msgs, YAMLFormat, false)

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

func TestFormatter_PrintEmpty(t *testing.T) {
	g := NewWithT(t)

	msgs := Messages{}

	logOutput, _ := PrintLog(msgs, false)
	g.Expect(logOutput).To(Equal(""))

	jsonOutput, _ := PrintJSON(msgs)
	g.Expect(jsonOutput).To(Equal("[]"))

	yamlOutput, _ := PrintYAML(msgs)
	g.Expect(yamlOutput).To(Equal("[]\n"))
}
