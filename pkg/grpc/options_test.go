// Copyright 2019 Istio Authors
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

package grpc_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/grpc"
)

// Test default maximum connection age is set to infinite, preserving previous
// unbounded lifetime behavior.
func TestAgeDefaultsToInfinite(t *testing.T) {
	ko := grpc.DefaultOption()

	if ko.MaxServerConnectionAge != grpc.Infinity {
		t.Errorf("%s maximum connection age %v", t.Name(), ko.MaxServerConnectionAge)
	}
}

// Confirm maximum connection age parameters can be set from the command line.
func TestSetConnectionAgeCommandlineOptions(t *testing.T) {
	ko := grpc.DefaultOption()
	cmd := &cobra.Command{}
	ko.AttachCobraFlags(cmd)

	buf := new(bytes.Buffer)
	cmd.SetOutput(buf)
	sec := 1 * time.Second
	cmd.SetArgs([]string{
		fmt.Sprintf("--keepaliveMaxServerConnectionAge=%v", sec),
	})

	if err := cmd.Execute(); err != nil {
		t.Errorf("%s %s", t.Name(), err.Error())
	}
	if ko.MaxServerConnectionAge != sec {
		t.Errorf("%s maximum connection age %v", t.Name(), ko.MaxServerConnectionAge)
	}
}

// Test default wirte/read buffer connection age is set to 32k, preserving previous
// unbounded lifetime behavior.
func TestBufferSizeDefaultSetting(t *testing.T) {
	ko := grpc.DefaultOption()

	if ko.WriteBufferSize != 32*1024 {
		t.Errorf("%s write buffer size %v", t.Name(), ko.WriteBufferSize)
	}
	if ko.ReadBufferSize != 32*1024 {
		t.Errorf("%s read buffer size %v", t.Name(), ko.WriteBufferSize)
	}
}

// Confirm wirte/read buffer size can be set from the command line.
func TestSetBufferSizeCommandlineOptions(t *testing.T) {
	ko := grpc.DefaultOption()
	cmd := &cobra.Command{}
	ko.AttachCobraFlags(cmd)

	buf := new(bytes.Buffer)
	cmd.SetOutput(buf)
	writeSize := 16 * 1024
	readSize := 64 * 1024
	cmd.SetArgs([]string{
		fmt.Sprintf("--writeBufferSize=%d", writeSize),
		fmt.Sprintf("--readBufferSize=%d", readSize),
	})

	if err := cmd.Execute(); err != nil {
		t.Errorf("%s %s", t.Name(), err.Error())
	}
	if ko.WriteBufferSize != writeSize {
		t.Errorf("%s write buffer size %v", t.Name(), ko.WriteBufferSize)
	}
	if ko.ReadBufferSize != readSize {
		t.Errorf("%s read buffer size %v", t.Name(), ko.ReadBufferSize)
	}
}
