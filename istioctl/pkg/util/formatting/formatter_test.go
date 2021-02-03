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

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/pkg/url"
)

func TestFormatter_PrintLog(t *testing.T) {
	g := NewWithT(t)

	firstMsg := diag.NewMessage(
		diag.NewMessageType(diag.Error, "B1", "Explosion accident: %v"),
		diag.MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := diag.NewMessage(
		diag.NewMessageType(diag.Warning, "C1", "Collapse danger: %v"),
		diag.MockResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := diag.Messages{firstMsg, secondMsg}
	output, _ := Print(msgs, LogFormat, false)

	g.Expect(output).To(Equal(
		"Error [B1] (SoapBubble) Explosion accident: the bubble is too big\n" +
			"Warning [C1] (GrandCastle) Collapse danger: the castle is too old",
	))
}

func TestFormatter_PrintLogWithColor(t *testing.T) {
	g := NewWithT(t)

	firstMsg := diag.NewMessage(
		diag.NewMessageType(diag.Error, "B1", "Explosion accident: %v"),
		diag.MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := diag.NewMessage(
		diag.NewMessageType(diag.Warning, "C1", "Collapse danger: %v"),
		diag.MockResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := diag.Messages{firstMsg, secondMsg}
	output, _ := Print(msgs, LogFormat, true)

	g.Expect(output).To(Equal(
		"\033[1;31mError\033[0m [B1] (SoapBubble) Explosion accident: the bubble is too big\n" +
			"\033[33mWarning\033[0m [C1] (GrandCastle) Collapse danger: the castle is too old",
	))
}

func TestFormatter_PrintJSON(t *testing.T) {
	g := NewWithT(t)

	firstMsg := diag.NewMessage(
		diag.NewMessageType(diag.Error, "B1", "Explosion accident: %v"),
		diag.MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := diag.NewMessage(
		diag.NewMessageType(diag.Warning, "C1", "Collapse danger: %v"),
		diag.MockResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := diag.Messages{firstMsg, secondMsg}
	output, _ := Print(msgs, JSONFormat, false)

	expectedOutput := `[
	{
		"code": "B1",
		"documentationUrl": "` + url.ConfigAnalysis + `/b1/",
		"level": "Error",
		"message": "Explosion accident: the bubble is too big",
		"origin": "SoapBubble"
	},
	{
		"code": "C1",
		"documentationUrl": "` + url.ConfigAnalysis + `/c1/",
		"level": "Warning",
		"message": "Collapse danger: the castle is too old",
		"origin": "GrandCastle"
	}
]`

	g.Expect(output).To(Equal(expectedOutput))
}

func TestFormatter_PrintYAML(t *testing.T) {
	g := NewWithT(t)

	firstMsg := diag.NewMessage(
		diag.NewMessageType(diag.Error, "B1", "Explosion accident: %v"),
		diag.MockResource("SoapBubble"),
		"the bubble is too big",
	)
	secondMsg := diag.NewMessage(
		diag.NewMessageType(diag.Warning, "C1", "Collapse danger: %v"),
		diag.MockResource("GrandCastle"),
		"the castle is too old",
	)

	msgs := diag.Messages{firstMsg, secondMsg}
	output, _ := Print(msgs, YAMLFormat, false)

	expectedOutput := `- code: B1
  documentationUrl: ` + url.ConfigAnalysis + `/b1/
  level: Error
  message: 'Explosion accident: the bubble is too big'
  origin: SoapBubble
- code: C1
  documentationUrl: ` + url.ConfigAnalysis + `/c1/
  level: Warning
  message: 'Collapse danger: the castle is too old'
  origin: GrandCastle
`

	g.Expect(output).To(Equal(expectedOutput))
}

func TestFormatter_PrintEmpty(t *testing.T) {
	g := NewWithT(t)

	msgs := diag.Messages{}

	logOutput, _ := Print(msgs, LogFormat, false)
	g.Expect(logOutput).To(Equal(""))

	jsonOutput, _ := Print(msgs, JSONFormat, false)
	g.Expect(jsonOutput).To(Equal("[]"))

	yamlOutput, _ := Print(msgs, YAMLFormat, false)
	g.Expect(yamlOutput).To(Equal("[]\n"))
}
