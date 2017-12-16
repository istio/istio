// Copyright 2017 Istio Authors
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

package runtime

type protocol int

const (
	protocolHTTP protocol = 1 << iota
	protocolTCP
)

type method int

const (
	methodCheck method = 1 << iota
	methodReport
	methodPreprocess
	methodQuota
)

// ResourceType codifies types of resources.
// resources apply to certain protocols or methods.
type ResourceType struct {
	protocol protocol
	method   method
}

// String return string presentation of const.
func (r ResourceType) String() string {
	p := ""
	if r.IsHTTP() {
		p += "HTTP "
	}
	if r.IsTCP() {
		p += "TCP "
	}
	m := ""
	if r.IsCheck() {
		m += "Check "
	}
	if r.IsReport() {
		m += "Report "
	}
	if r.IsQuota() {
		m += "Quota"
	}
	if r.IsPreprocess() {
		m += "Preprocess"
	}
	return "ResourceType:{" + p + "/" + m + "}"
}

// IsTCP returns true if resource is for TCP
func (r ResourceType) IsTCP() bool {
	return r.protocol&protocolTCP != 0
}

// IsHTTP returns true if resource is for HTTP
func (r ResourceType) IsHTTP() bool {
	return r.protocol&protocolHTTP != 0
}

// IsCheck returns true if resource is for HTTP
func (r ResourceType) IsCheck() bool {
	return r.method&methodCheck != 0
}

// IsReport returns true if resource is for Report
func (r ResourceType) IsReport() bool {
	return r.method&methodReport != 0
}

// IsPreprocess returns true if resource is for Preprocess
func (r ResourceType) IsPreprocess() bool {
	return r.method&methodPreprocess != 0
}

// IsQuota returns true if resource is for IsQuota
func (r ResourceType) IsQuota() bool {
	return r.method&methodQuota != 0
}

// defaultResourcetype defines the resource type if nothing is specified.
func defaultResourcetype() ResourceType {
	return ResourceType{
		protocol: protocolHTTP,
		method:   methodCheck | methodReport | methodPreprocess,
	}
}
