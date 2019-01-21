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

package server

import (
	"sync"
)

var goRoutines = make(map[string]int64)
var goRoutinesMutex sync.RWMutex

func goWithDebug(name string, fn func()) {
	if !scope.DebugEnabled() {
		go fn()
		return
	}

	go func() {
		goRoutinesMutex.Lock()
		v := goRoutines[name]
		v++
		goRoutines[name] = v
		goRoutinesMutex.Unlock()

		fn()

		goRoutinesMutex.Lock()
		v = goRoutines[name]
		v--
		goRoutines[name] = v
		goRoutinesMutex.Unlock()
	}()
}


//func getGID() uint64 {
//	b := make([]byte, 64)
//	b = b[:runtime.Stack(b, false)]
//	b = bytes.TrimPrefix(b, []byte("goroutine "))
//	b = b[:bytes.IndexByte(b, ' ')]
//	n, _ := strconv.ParseUint(string(b), 10, 64)
//	return n
//}

//func DumpActiveGoroutines() {
//	fmt.Printf("**** Fan-in watch count: %d\n", fanInWatchCount)
//
//	i := 0
//	fmt.Printf("======== Active GoRoutines ======\n")
//	activeGoroutines.Range(func(key, value interface{}) bool {
//		//		fmt.Printf("[%d] *** ACTIVE GoRoutine(%d): %s\n", i, key, value)
//		i++
//		return true
//	})
//	fmt.Printf("======== DONE Active GoRoutines (%d) ======\n", i)
//}
