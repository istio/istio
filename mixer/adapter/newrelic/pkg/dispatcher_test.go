// Copyright 2018 Istio Authors
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
package pkg

import (
	"testing"
	"time"
)

func TestStartDispatcher(t *testing.T) {
	StartDispatcher(1024)
	time.Sleep(time.Second * 5)
	lenOfWPool := len(WorkerPool)
	if lenOfWPool != 1024 {
		t.Errorf("length of WorkerPool is not 1024. it is%d", lenOfWPool)
	} else {
		t.Logf("initialized %d workers", lenOfWPool)
	}
}
