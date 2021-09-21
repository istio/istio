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

package log

type Command struct {
	// Short identifier that shows up in NFLOG output. Must be less than 64 characters
	Identifier string
	Comment    string
}

var (
	JumpInbound             = Command{"JumpInbound", "direct all traffic through ISTIO_INBOUND chain"}
	JumpOutbound            = Command{"JumpOutbound", "direct all traffic through ISTIO_OUTBOUND chain"}
	ExcludeInboundPort      = Command{"ExcludeInboundPort", "exclude inbound port from capture"}
	IncludeInboundPort      = Command{"IncludeInboundPort", "include inbound port for capture"}
	InboundCapture          = Command{"InboundCapture", "redirect inbound request to proxy"}
	KubevirtCommand         = Command{"KubevirtCommand", "Kubevirt outbound redirect"}
	ExcludeInterfaceCommand = Command{"ExcludeInterfaceCommand", "Excluded interface"}
	UndefinedCommand        = Command{"UndefinedCommand", ""}
)

var IDToCommand = map[string]Command{
	"JumpInbound":             JumpInbound,
	"JumpOutbound":            JumpOutbound,
	"ExcludeInboundPort":      ExcludeInboundPort,
	"IncludeInboundPort":      IncludeInboundPort,
	"InboundCapture":          InboundCapture,
	"KubevirtCommand":         KubevirtCommand,
	"ExcludeInterfaceCommand": ExcludeInterfaceCommand,
	"UndefinedCommand":        UndefinedCommand,
}
