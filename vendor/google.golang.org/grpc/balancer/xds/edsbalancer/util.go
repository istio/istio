// +build go1.12

/*
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package edsbalancer

import (
	"sync"
)

type dropper struct {
	// Drop rate will be numerator/denominator.
	numerator   uint32
	denominator uint32

	mu sync.Mutex
	i  uint32
}

func newDropper(numerator, denominator uint32) *dropper {
	return &dropper{
		numerator:   numerator,
		denominator: denominator,
	}
}

func (d *dropper) drop() (ret bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// TODO: the drop algorithm needs a design.
	// Currently, for drop rate 3/5:
	// 0 1 2 3 4
	// d d d n n
	if d.i < d.numerator {
		ret = true
	}
	d.i++
	if d.i >= d.denominator {
		d.i = 0
	}

	return
}
