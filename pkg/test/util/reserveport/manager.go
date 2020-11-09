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

import (
	"fmt"
	"sync"
	"testing"
)

type managerImpl struct {
	pool  []ReservedPort
	index int
	mutex sync.Mutex
}

func (m *managerImpl) ReservePort() (ReservedPort, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.index >= len(m.pool) {
		// Re-create the pool.
		var err error
		m.pool, err = allocatePool(poolSize)
		if err != nil {
			return nil, err
		}
		m.index = 0
		// Don't need to free the pool, since all ports have been reserved.
	}

	p := m.pool[m.index]
	if p == nil {
		return nil, fmt.Errorf("attempting to reserve port after manager closed")
	}
	m.pool[m.index] = nil
	m.index++
	return p, nil
}

func (m *managerImpl) ReservePortOrFail(t *testing.T) ReservedPort {
	t.Helper()
	p, err := m.ReservePort()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (m *managerImpl) ReservePortNumber() (uint16, error) {
	p, err := m.ReservePort()
	if err != nil {
		return 0, err
	}
	n := p.GetPort()
	if err := p.Close(); err != nil {
		return 0, err
	}
	return n, nil
}

func (m *managerImpl) ReservePortNumberOrFail(t *testing.T) uint16 {
	t.Helper()
	p, err := m.ReservePortNumber()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (m *managerImpl) Close() (err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	pool := m.pool
	m.pool = nil
	m.index = 0
	return freePool(pool)
}

func (m *managerImpl) CloseSilently() {
	_ = m.Close()
}
