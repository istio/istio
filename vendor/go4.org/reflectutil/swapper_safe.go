// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build js appengine safe ppc64 ppc64le arm64 mips mipsle mips64 mips64le

package reflectutil

import "reflect"

func swapper(slice reflect.Value) func(i, j int) {
	tmp := reflect.New(slice.Type().Elem()).Elem()
	return func(i, j int) {
		v1 := slice.Index(i)
		v2 := slice.Index(j)
		tmp.Set(v1)
		v1.Set(v2)
		v2.Set(tmp)
	}
}
