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

package reserveport

import multierror "github.com/hashicorp/go-multierror"

func allocatePool(size int) (pool []ReservedPort, err error) {
	tempPool := make([]ReservedPort, size)
	defer func() {
		if err != nil {
			_ = freePool(tempPool)
		}
	}()

	for i := 0; i < size; i++ {
		var err error
		tempPool[i], err = newReservedPort()
		if err != nil {
			return nil, err
		}
	}
	return tempPool, nil
}

func freePool(pool []ReservedPort) (err error) {
	// Close any ports still in the pool.
	for i := 0; i < len(pool); i++ {
		p := pool[i]
		if p == nil {
			continue
		}

		if e := p.Close(); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return err
}
