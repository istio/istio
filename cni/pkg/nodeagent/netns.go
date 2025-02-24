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

import "io"

type NetnsFd interface {
	Fd() uintptr
}

type Netns interface {
	NetnsFd
	Inode() uint64
	OwnerProcStarttime() uint64
}
type NetnsCloser interface {
	io.Closer
	Netns
}

type NetnsWithFd struct {
	netns              io.Closer
	fd                 uintptr
	inode              uint64
	ownerProcStarttime uint64
}

func (n *NetnsWithFd) Close() error {
	if n.netns == nil {
		return nil
	}

	ret := n.netns.Close()
	// set fd to invalid value, so if something uses it by mistake it will err
	uintZero := uintptr(0)
	n.fd = ^uintZero
	return ret
}

func (n *NetnsWithFd) Fd() uintptr {
	return n.fd
}

func (n *NetnsWithFd) Inode() uint64 {
	return n.inode
}

func (n *NetnsWithFd) OwnerProcStarttime() uint64 {
	return n.ownerProcStarttime
}
