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
	"os/user"
	"strings"
)

type StdoutStubDependencies struct {
}

func (s *StdoutStubDependencies) GetLocalIP() (net.IP, error) {
	return net.IPv4(127, 0, 0, 1), nil
}

func (s *StdoutStubDependencies) LookupUser() (*user.User, error) {
	return &user.User{Uid: "0"}, nil
}

func (s *StdoutStubDependencies) RunOrFail(cmd Cmd, args ...string) {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
}

func (s *StdoutStubDependencies) Run(cmd Cmd, args ...string) error {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
	return nil
}

func (s *StdoutStubDependencies) RunQuietlyAndIgnore(cmd Cmd, args ...string) {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
}
