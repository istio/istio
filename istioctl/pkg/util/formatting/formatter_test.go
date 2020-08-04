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

package formatting

import (
	"testing"

	. "github.com/onsi/gomega"

	. "istio.io/istio/galley/pkg/config/analysis/diag"
)

func TestFormatter_RenderWithColor(t *testing.T) {
	g := NewWithT(t)
	mt := NewMessageType(Error, "IST-0042", "Cheese type not found: %q")
	m := NewMessage(mt, nil, "Feta")

	g.Expect(render(m, true)).To(Equal(
		colorPrefixes[Error] + "Error\033[0m [IST-0042] Cheese type not found: \"Feta\""))
}

func TestFormatter_PrintLog(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Explosion accident: %v"),
		MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Collapse danger: %v"),
		MockResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := Messages{firstMsg, secondMsg}
	output, _ := Print(msgs, LogFormat, false)

	g.Expect(output).To(Equal(
		"Error [B1] (SoapBubble) Explosion accident: the bubble is too big\n" +
			"Warn [C1] (GrandCastle) Collapse danger: the castle is too old\n",
	))
}

func TestFormatter_PrintJSON(t *testing.T) {
	g := NewWithT(t)

	firstMsg := NewMessage(
		NewMessageType(Error, "B1", "Explosion accident: %v"),
		MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Collapse danger: %v"),
		MockResource("GrandCastle"),
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
		MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := NewMessage(
		NewMessageType(Warning, "C1", "Collapse danger: %v"),
		MockResource("GrandCastle"),
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

	logOutput, _ := Print(msgs, LogFormat, false)
	g.Expect(logOutput).To(Equal(""))

	jsonOutput, _ := Print(msgs, JSONFormat, false)
	g.Expect(jsonOutput).To(Equal("[]"))

	yamlOutput, _ := Print(msgs, YAMLFormat, false)
	g.Expect(yamlOutput).To(Equal("[]\n"))
}
