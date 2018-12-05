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

	if a.MeshConfigFile != defaultMeshConfigFile {
		t.Fatalf("unexpected MeshConfigFile: %s", a.MeshConfigFile)
	}

	if a.DomainSuffix != defaultDomainSuffix {
		t.Fatalf("unexpected DomainSuffix: %s", a.DomainSuffix)
	}

	if a.Insecure {
		t.Fatal("Default of Insecure should be false")
	}
}

func TestArgs_String(t *testing.T) {
	a := DefaultArgs()
	// Should not crash
	_ = a.String()
}

func TestValidate(t *testing.T) {
	a := DefaultArgs()
	if err := a.Validate(); err != nil {
		t.Fatalf("DefaultArgs() is not valid: %v", err)
	}

	a = DefaultArgs()
	a.APIAddress = "localhost:9901"
	if err := a.Validate(); err != nil {
		t.Fatal("APIAddress=localhost:9901 should not fail validation")
	}

	a = DefaultArgs()
	a.CredentialOptions.KeyFile = ""
	if err := a.Validate(); err == nil {
		t.Fatal("Empty CredentialOptions.KeyFile  should fail validation")
	}
	a.Insecure = true
	if err := a.Validate(); err != nil {
		t.Fatalf("Empty CredentialOptions.KeyFile with Insecure should not fail validation: %v", err)
	}

	a = DefaultArgs()
	a.AccessListFile = ""
	if err := a.Validate(); err == nil {
		t.Fatalf("Empty CredentialOptions.KeyFile with Insecure and empty accesslist should fail validation: %v", err)
	}

	a = DefaultArgs()
	a.MeshConfigFile = ""
	if err := a.Validate(); err == nil {
		t.Fatal("Empty MeshConfigFile should fail validation")
	}

	a = DefaultArgs()
	a.LoggingOptions.OutputPaths = []string{}
	if err := a.Validate(); err == nil {
		t.Fatal("Unset LoggingOptions.OutputPaths should fail validation")
	}

	a = DefaultArgs()
	a.APIAddress = "http://us\ner:pass\nword@foo.com/" // invalid URL
	if err := a.Validate(); err == nil {
		t.Fatal("Invalid APIAddress should fail validation")
	}

	a = DefaultArgs()
	a.EnableServer = false
	a.CredentialOptions.KeyFile = ""
	if err := a.Validate(); err != nil {
		t.Fatalf("Invalid argument validation should be skipped if server is disabled: %v", err)
	}
}
