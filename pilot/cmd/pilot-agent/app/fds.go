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

package app

// RaiseFileLimits sets the file limit to the maximum allowed, to avoid exhaustion of file descriptors.
// This is done by setting soft limit == hard limit.
// Typical container runtimes already do this, but on VMs, etc this is generally not the case, and a limit of 1024 is common -- this is quite low!
//
// Go already sets this (https://github.com/golang/go/issues/46279).
// However, it will restore the original limit for subprocesses (Envoy):
// https://github.com/golang/go/blob/f0d1195e13e06acdf8999188decc63306f9903f5/src/syscall/rlimit.go#L14.
// By explicitly doing it ourselves, we get the limit passed through to Envoy.
//
// This function returns the new limit additionally, for convenience.
func RaiseFileLimits() (uint64, error) {
	return raiseFileLimits()
}
