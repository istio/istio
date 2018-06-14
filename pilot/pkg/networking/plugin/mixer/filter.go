// Copyright 2018 Istio Authors
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

package mixer

import (
	"net"

	mpb "istio.io/api/mixer/v1"
)

const (
	// AttrIPSuffix represents IP address suffix.
	AttrIPSuffix = "ip"

	// AttrUIDSuffix is the uid suffix of with source or destination.
	AttrUIDSuffix = "uid"

	// AttrLabelsSuffix is the suffix for labels associated with source or destination.
	AttrLabelsSuffix = "labels"

	// AttrDestinationPrefix all destination attributes start with this prefix
	AttrDestinationPrefix = "destination"

	// AttrSourcePrefix all source attributes start with this prefix
	AttrSourcePrefix = "source"

	// MixerFilter name and its attributes
	MixerFilter = "mixer"

	// AttrDestinationService is name of the target service
	AttrDestinationService = "destination.service"
)

// AddStandardNodeAttributes add standard node attributes with the given prefix
func AddStandardNodeAttributes(attr map[string]*mpb.Attributes_AttributeValue, prefix string, IPAddress string, ID string, labels map[string]string) {
	if len(IPAddress) > 0 {
		attr[prefix+"."+AttrIPSuffix] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_BytesValue{net.ParseIP(IPAddress)},
		}
	}

	attr[prefix+"."+AttrUIDSuffix] = &mpb.Attributes_AttributeValue{
		Value: &mpb.Attributes_AttributeValue_StringValue{"kubernetes://" + ID},
	}

	if len(labels) > 0 {
		attr[prefix+"."+AttrLabelsSuffix] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_StringMapValue{
				StringMapValue: &mpb.Attributes_StringMap{Entries: labels},
			},
		}
	}
}

// StandardNodeAttributes populates and returns a map of attributes with the provided parameters.
func StandardNodeAttributes(prefix string, IPAddress string, ID string, labels map[string]string) map[string]*mpb.Attributes_AttributeValue {
	attrs := make(map[string]*mpb.Attributes_AttributeValue)
	AddStandardNodeAttributes(attrs, prefix, IPAddress, ID, labels)
	return attrs
}
