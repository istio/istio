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

package main

import "fmt"
import "reflect"
import "sort"
import "gopkg.in/d4l3k/messagediff.v1"

func main() {
	map1 := map[string][]string{
		"key1": {"value1", "value2"},
		"key2": {"value2", "value1"}}
	map2 := map[string][]string{
		"key2": {"value1", "value2"},
		"key1": {"value", "value2"}}

	for _, v := range map1 {
		sort.Strings(v)
	}
	/*	for _, v := range map2 {
			sort.Strings(v)
		}
	*/
	if !reflect.DeepEqual(map1, map2) {
		fmt.Println("not match")
	} else {
		fmt.Println("match")
	}
	fmt.Println("v%", map1)
	diff, equal := messagediff.PrettyDiff(map1, map2)
	if !equal {
		fmt.Println(diff)
	}
	fmt.Println("v%", "ranoss_mml-handler_172.168.40.180_12181" > "ranoss_discover-controller_172.168.40.120_12290")
	fmt.Println("v%", "ranoss_mml-handler_172.168.40.180_12181" < "ranoss_discover-controller_172.168.40.120_12290")
}
