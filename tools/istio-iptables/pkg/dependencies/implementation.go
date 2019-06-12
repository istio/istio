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

package dependencies

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"istio.io/pkg/env"
)

// RealDependencies implementation of interface Dependencies, which is used in production
type RealDependencies struct {
}

// GetLocalIP returns the local IP address
func (r *RealDependencies) GetLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			return ipnet.IP, nil
		}
	}
	return nil, fmt.Errorf("no valid local IP address found")
}

// LookupUser returns user, which runs this executable
func (r *RealDependencies) LookupUser() (*user.User, error) {
	username := env.RegisterStringVar("ENVOY_USER", "istio-proxy", "User used for iptable rules").Get()

	return user.Lookup(username)
}

func (r *RealDependencies) execute(cmd Cmd, redirectStdout bool, args ...string) error {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
	externalCommand := exec.Command(string(cmd), args...)
	externalCommand.Stdout = os.Stdout
	//TODO Check naming and redirection logic
	if !redirectStdout {
		externalCommand.Stderr = os.Stderr
	}
	return externalCommand.Run()
}

// RunOrFail runs a command and panics, if it fails
func (r *RealDependencies) RunOrFail(cmd Cmd, args ...string) {
	err := r.execute(cmd, false, args...)

	if err != nil {
		panic(err)
	}
}

// Run runs a command
func (r *RealDependencies) Run(cmd Cmd, args ...string) error {
	return r.execute(cmd, false, args...)
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (r *RealDependencies) RunQuietlyAndIgnore(cmd Cmd, args ...string) {
	_ = r.execute(cmd, true, args...)
}
