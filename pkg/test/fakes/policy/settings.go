//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package policy

import (
	"fmt"
	"strconv"
	"time"
)

type settings map[string]string

func newSettings() settings {
	return settings(make(map[string]string))
}

func (s settings) setDenyCheck(value bool) settings {
	return s.setBool("denyCheck", value)
}

func (s settings) getDenyCheck() bool {
	return s.getBoolOrDefault("denyCheck")
}

func (s settings) setValidDuration(d time.Duration) {
	s.setString("validDuration", d.String())
}

func (s settings) getValidDuration() time.Duration {
	if ds, ok := s["validDuration"]; !ok {
		d, _ := time.ParseDuration(ds)
		return d
	}
	return 5 * time.Second
}

func (s settings) setValidCount(d int32) {
	s.setString("validCount", string(d))
}

func (s settings) getValidCount() int32 {
	if cs, ok := s["validCount"]; !ok {
		c, _ := strconv.Atoi(cs)
		return int32(c)
	}
	return 1
}

func (s settings) setBool(name string, value bool) settings {
	v := "false"
	if value {
		v = "true"
	}
	return s.setString(name, v)
}

func (s settings) setString(name string, value string) settings {
	s[name] = value
	return s
}

func (s settings) getBoolOrDefault(name string) bool {
	str := s[name]
	switch str {
	case "", "0", "false":
		return false

	case "1", "true":
		return true

	default:
		panic(fmt.Sprintf("Unexpected bool value string: %s", str))
	}
}
