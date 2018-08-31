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

import (
	"errors"
	"net"
	"testing"
	"time"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/log"
)

func TestNewServer_Errors(t *testing.T) {

loop:
	for i := 0; ; i++ {
		p := defaultPatchTable()
		mk := mock.NewKube()
		p.newKubeFromConfigFile = func(string) (kube.Interfaces, error) { return mk, nil }
		p.newSource = func(kube.Interfaces, time.Duration) (runtime.Source, error) {
			return runtime.NewInMemorySource(), nil
		}

		e := errors.New("err")

		switch i {
		case 0:
			p.logConfigure = func(*log.Options) error { return e }
		case 1:
			p.newKubeFromConfigFile = func(string) (kube.Interfaces, error) { return nil, e }
		case 2:
			p.newSource = func(kube.Interfaces, time.Duration) (runtime.Source, error) { return nil, e }
		case 3:
			p.netListen = func(network, address string) (net.Listener, error) { return nil, e }
		default:
			break loop
		}

		args := DefaultArgs()
		args.APIAddress = "tcp://0.0.0.0:0"
		args.Insecure = true
		_, err := newServer(args, p)
		if err == nil {
			t.Fatalf("Expected error not found for i=%d", i)
		}
	}
}

func TestNewServer(t *testing.T) {
	p := defaultPatchTable()
	mk := mock.NewKube()
	p.newKubeFromConfigFile = func(string) (kube.Interfaces, error) { return mk, nil }
	p.newSource = func(kube.Interfaces, time.Duration) (runtime.Source, error) {
		return runtime.NewInMemorySource(), nil
	}

	args := DefaultArgs()
	args.APIAddress = "tcp://0.0.0.0:0"
	args.Insecure = true
	s, err := newServer(args, p)
	if err != nil {
		t.Fatalf("Unexpected error creating service: %v", err)
	}

	_ = s.Close()
	_ = s.Wait()
}

func TestServer_Basic(t *testing.T) {
	p := defaultPatchTable()
	mk := mock.NewKube()
	p.newKubeFromConfigFile = func(string) (kube.Interfaces, error) { return mk, nil }
	p.newSource = func(kube.Interfaces, time.Duration) (runtime.Source, error) {
		return runtime.NewInMemorySource(), nil
	}

	args := DefaultArgs()
	args.APIAddress = "tcp://0.0.0.0:0"
	args.Insecure = true
	s, err := newServer(args, p)
	if err != nil {
		t.Fatalf("Unexpected error creating service: %v", err)
	}

	s.Run()

	_ = s.Close()
	_ = s.Wait()
}
