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

package server

import "testing"

func TestDefaultArgs(t *testing.T) {
	a := DefaultArgs()

	// Spot check a few things
	if a.APIAddress != "tcp://0.0.0.0:9901" {
		t.Fatalf("unexpected APIAddress: %v", a.APIAddress)
	}

	if a.MaxReceivedMessageSize != 1024*1024 {
		t.Fatalf("unexpected MaxReceivedMessageSize: %d", a.MaxReceivedMessageSize)
	}

	if a.MaxConcurrentStreams != 1024 {
		t.Fatalf("unexpected MaxConcurrentStreams: %d", a.MaxConcurrentStreams)
	}
}

func TestArgs_String(t *testing.T) {
	a := DefaultArgs()
	// Should not crash
	_ = a.String()
}
