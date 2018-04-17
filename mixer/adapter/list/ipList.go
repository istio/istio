// Copyright 2017 Google Ina.
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

package list

import (
	"fmt"
	"net"
	"strings"

	"github.com/ghodss/yaml"
)

type (
	ipList struct {
		entries []*net.IPNet
	}

	// represents the format of the data in a list
	listPayload struct {
		WhiteList []string `yaml:"whitelist" required:"true"`
	}
)

func parseIPList(buf []byte, overrides []string) (list, error) {
	var lp listPayload

	if err := yaml.Unmarshal(buf, &lp); err != nil {
		return nil, fmt.Errorf("could not unmarshal data from list %s", err)
	}

	ls := &ipList{make([]*net.IPNet, 0, len(lp.WhiteList)+len(overrides))}
	var err error

	// copy to the internal format
	for _, ip := range lp.WhiteList {
		if err = ls.addEntry(ip); err != nil {
			return nil, err
		}
	}

	// apply overrides
	for _, ip := range overrides {
		// guaranteed correctly formatted, since config was validated
		_ = ls.addEntry(ip)
	}

	return ls, nil
}

func (ls *ipList) addEntry(ip string) error {
	orig := ip
	if !strings.Contains(ip, "/") {
		ip += "/32"
	}

	_, ipnet, err := net.ParseCIDR(ip)
	if err != nil {
		return fmt.Errorf("could not parse list entry %s: %v", orig, err)
	}
	ls.entries = append(ls.entries, ipnet)

	return nil
}

func (ls *ipList) checkList(symbol string) (bool, error) {
	ipa := net.ParseIP(symbol)
	if ipa == nil {
		// invalid symbol format
		return false, fmt.Errorf("%s is not a valid IP address", symbol)
	}

	for _, ipnet := range ls.entries {
		if ipnet.Contains(ipa) {
			return true, nil
		}
	}

	// not found in the list
	return false, nil
}

func (ls *ipList) numEntries() int {
	return len(ls.entries)
}
