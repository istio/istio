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

package builder

// IptablesProducer is an interface for adding iptables rules
type IptablesProducer interface {
	// AppendRuleV4 appends an IPv4 rule into the given iptables chain
	AppendRuleV4(chain string, table string, params ...string) IptablesProducer
	// AppendRuleV6 appends an IPv6 rule into the given iptables chain
	AppendRuleV6(chain string, table string, params ...string) IptablesProducer
	// InsertRuleV4 inserts IPv4 rule at a particular position in the chain
	InsertRuleV4(chain string, table string, position int, params ...string) IptablesProducer
	// InsertRuleV6 inserts IPv6 rule at a particular position in the chain
	InsertRuleV6(chain string, table string, position int, params ...string) IptablesProducer
}

// IptablesConsumer is an interface for constructing iptables-rules command string or iptables-restore formatted rules
type IptablesConsumer interface {
	// BuildV4 creates iptables commands
	BuildV4() [][]string
	// BuildV6 creates ip6tables commands
	BuildV6() [][]string
	// BuildV4Restore creates iptables-restore input format
	BuildV4Restore() string
	// BuildV6Restore creates ip6tables-restore input format
	BuildV6Restore() string
}

// IptablesBuilder is a higher level interface based on builder pattern.
type IptablesBuilder interface {
	IptablesProducer
	IptablesConsumer
}
