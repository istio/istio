//  Copyright Istio Authors
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

package reserveport

import (
	"net"
)

type portImpl struct {
	listener net.Listener
	port     uint16
}

func (p *portImpl) GetPort() uint16 {
	return p.port
}

func (p *portImpl) Close() error {
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

func (p *portImpl) CloseSilently() {
	_ = p.Close()
}

func newReservedPort() (port ReservedPort, err error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	// If any errors occur later in this method, close the listener.
	defer func() {
		if err != nil {
			_ = l.Close()
		}
	}()

	p := uint16(l.Addr().(*net.TCPAddr).Port)
	return &portImpl{
		listener: l,
		port:     p,
	}, nil
}
