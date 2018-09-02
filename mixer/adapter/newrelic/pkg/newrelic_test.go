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
	//"errors"
	"fmt"
	"testing"
)

func TestNewGrpcAdapter(t *testing.T) {
	s, err := NewGrpcAdapter("8080")

	if err != nil {
		t.Errorf("can't create new gRPC server")
	}

	if s.Addr() != "[::]:8080" {
		t.Errorf("address of adapter is not as expected")
	}

	shutdown := make(chan error, 1)
	go func() {
		s.Run(shutdown)
	}()

	err = s.Close()
	if err != nil {
		t.Errorf("can not close gRPC server")
	}
	fmt.Println("gRPC server has been shutdown")
}
