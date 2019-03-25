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

package model

import (
	"testing"
)

var myDestRules = map[string]*processedDestRules{
	"mns1": &processedDestRules{
		hosts:    make([]Hostname, 0),
		destRule: map[Hostname]*combinedDestinationRule{},
	},
	"mns2": &processedDestRules{
		hosts:    make([]Hostname, 0),
		destRule: map[Hostname]*combinedDestinationRule{},
	},
}

func TestDestinationRule(t *testing.T) {
	ps := NewPushContext()

	data := []struct {
		host                Hostname
		namespace           string
		destinationRuleName string
	}{
		{
			host:                "*.local",
			namespace:           "mns1",
			destinationRuleName: "drname1",
		},
		{
			host:                "host1.svc.cluster.local",
			namespace:           "mns2",
			destinationRuleName: "drname2",
		},
		{
			host:                "*.svc.cluster.local",
			namespace:           "mns2",
			destinationRuleName: "drname3",
		},
	}

	for _, d := range data {
		myDestRules[d.namespace].hosts = append(myDestRules[d.namespace].hosts, d.host)
		myDestRules[d.namespace].destRule[d.host] = &combinedDestinationRule{
			subsets: make(map[string]struct{}),
			config: &Config{
				ConfigMeta: ConfigMeta{
					Type:      DestinationRule.Type,
					Name:      d.destinationRuleName,
					Namespace: d.namespace,
				},
			},
		}
	}

	ps.namespaceLocalDestRules = myDestRules

	cases := []struct {
		hostname   Hostname
		expectName string
	}{
		{
			hostname:   "host1.svc.cluster.local",
			expectName: "drname2",
		},
	}

	for _, c := range cases {
		myservice := &Service{Hostname: c.hostname}
		myconfig := ps.DestinationRule(nil, myservice)
		if myconfig.ConfigMeta.Name != c.expectName {
			t.Errorf("Destination Rule Got: %v Expected: %v\n", myconfig.ConfigMeta.Name, c.expectName)
		}
	}
}
