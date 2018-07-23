//  Copyright 2018 Istio Authors
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

package kube

import (
	"errors"
	"time"
)

const defaultTimeout = time.Second * 20
const defaultRetryWait = time.Millisecond * 10

// retry the given function, until there is a timeout, or until the function indicates that it has completed.
func retry(
	timeout time.Duration,
	retryWait time.Duration,
	fn func() (result interface{}, completed bool, err error)) (interface{}, error) {

	to := time.After(timeout)
	for {
		select {
		case <-to:
			return nil, errors.New("timeout while waiting")
		default:
		}

		result, completed, err := fn()
		if completed {
			return result, err
		}

		<-time.After(retryWait)
	}
}
