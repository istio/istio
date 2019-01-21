//  Copyright 2019 Istio Authors
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

package debug

import (
	"fmt"
	"sync"

)

var (
	mu     sync.Mutex
	active = make(map[string]int64)
)

// Go wraps "go fn()", to provide diagnostic output.
func Go(name string, fn func()) {
	//if !log.DebugEnabled() {
	//	go fn()
	//	return
	//}

	go func() {
		inc(name)
		defer dec(name)
		fn()
	}()
}

// Dump returns a string dump of currently active go routines.
func Dump() string {
	s := ""

	mu.Lock()
	defer mu.Unlock()

	s += fmt.Sprintf("*** GoRoutine Dump ***\n")
	for k, v := range active {
		s += fmt.Sprintf("go(%q):\t%d\n", k, v)
	}
	s += fmt.Sprintf("*** End GoRoutine Dump ***\n")

	return s
}

func inc(name string) {
	mu.Lock()
	v := active[name]
	v++
	active[name] = v
	mu.Unlock()
}

func dec(name string) {
	mu.Lock()
	v := active[name]
	v--
	if v == 0 {
		delete(active, name)
	} else {
		active[name] = v
	}

	mu.Unlock()
}
