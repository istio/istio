// Copyright Istio Authors.
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

package multicluster

import (
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

type ConditionFunc func() (done bool, err error)

type Poller interface {
	Poll(interval, timeout time.Duration, condition ConditionFunc) error
}

func NewPoller() Poller {
	return pollerImpl{}
}

var _ Poller = pollerImpl{}

type pollerImpl struct{}

func (p pollerImpl) Poll(interval, timeout time.Duration, condition ConditionFunc) error {
	return wait.Poll(interval, timeout, func() (bool, error) {
		return condition()
	})
}
