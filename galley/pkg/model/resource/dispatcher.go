////  Copyright 2018 Istio Authors
////
////  Licensed under the Apache License, Version 2.0 (the "License");
////  you may not use this file except in compliance with the License.
////  You may obtain a copy of the License at
////
////      http://www.apache.org/licenses/LICENSE-2.0
////
////  Unless required by applicable law or agreed to in writing, software
////  distributed under the License is distributed on an "AS IS" BASIS,
////  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
////  See the License for the specific language governing permissions and
////  limitations under the License.
//
package resource

//
//import "fmt"
//
//// TODO: Experimental
//
//type DispatchFn func(r Entry) error
//
//type Dispatcher struct {
//	fns       map[Kind]DispatchFn
//	defaultFn DispatchFn
//}
//
//func (d *Dispatcher) Dispatch(r Entry) error {
//	if fn, ok := d.fns[r.Id.Kind]; ok {
//		return fn(r)
//	}
//
//	if d.defaultFn != nil {
//		return d.defaultFn(r)
//	}
//
//	return fmt.Errorf("no dispatcher found: %v", r.Id.Kind)
//}
//
//func (d *Dispatcher) SetDefault(fn DispatchFn) {
//	d.defaultFn = fn
//}
//
//func (d *Dispatcher) Set(kind Kind, fn DispatchFn) {
//	d.fns[kind] = fn
//}
