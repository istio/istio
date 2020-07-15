// Copyright Istio Authors
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

package platform

import (
	"sync"
	"time"
)

const defaultTimeout = 5 * time.Second
const Platforms = 3

// Discover attempts to discover the host platform, defaulting to
// `Unknown` if a platform cannot be discovered.
func Discover() Environment {
	return DiscoverWithTimeout(defaultTimeout)
}

// DiscoverWithTimeout attempts to discover the host platform, defaulting to
// `Unknown` after the provided timeout.
func DiscoverWithTimeout(timeout time.Duration) Environment {
	plat := make(chan Environment, Platforms) // sized to match number of platform goroutines
	done := make(chan bool)

	var wg sync.WaitGroup
	wg.Add(Platforms) // check GCP, AWS, and Azure

	go func() {
		if IsGCP() {
			plat <- NewGCP()
		}
		wg.Done()
	}()

	go func() {
		if IsAWS() {
			plat <- NewAWS()
		}
		wg.Done()
	}()

	go func() {
		if IsAzure() {
			plat <- NewAzure()
		}
		wg.Done()
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case p := <-plat:
		return p
	case <-done:
		select {
		case p := <-plat:
			return p
		default:
			return &Unknown{}
		}
	case <-timer.C:
		return &Unknown{}
	}
}
