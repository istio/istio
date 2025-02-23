//go:build !linux && !windows
// +build !linux,!windows

// Copyright Istio Authors
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

package nodeagent

import "errors"

func inodeForFd(_ NetnsFd) (uint64, error) {
	return 0, errors.New("not implemented")
}

func NetnsSet(n NetnsFd) error {
	return errors.New("not implemented")
}

func OpenNetns(nspath string) (NetnsCloser, error) {
	return nil, errors.New("not implemented")
}

// inspired by netns.Do() but with an existing fd.
func NetnsDo(fdable NetnsFd, toRun func() error) error {
	return errors.New("not implemented")
}
