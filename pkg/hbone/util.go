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

package hbone

import (
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	istiolog "istio.io/pkg/log"
)

// createBuffer to get a buffer. io.Copy uses 32k.
// experimental use shows ~20k max read with Firefox.
var bufferPoolCopy = sync.Pool{New: func() interface{} {
	return make([]byte, 0, 32*1024)
}}

func copyBuffered(dst io.Writer, src io.Reader, log *istiolog.Scope) {
	buf1 := bufferPoolCopy.Get().([]byte)
	// nolint: staticcheck
	defer bufferPoolCopy.Put(buf1)
	bufCap := cap(buf1)
	buf := buf1[0:bufCap:bufCap]

	// For netstack: src is a gonet.Conn, doesn't implement WriterTo. Dst is a net.TcpConn - and implements ReadFrom.
	// CopyBuffered is the actual implementation of Copy and CopyBuffer.
	// if buf is nil, one is allocated.
	// Duplicated from io

	// This will prevent stats from working.
	// If the reader has a WriteTo method, use it to do the copy.
	// Avoids an allocation and a copy.
	//if wt, ok := src.(io.WriterTo); ok {
	//	return wt.WriteTo(dst)
	//}
	// Similarly, if the writer has a ReadFrom method, use it to do the copy.
	//if rt, ok := dst.(io.ReaderFrom); ok {
	//	return rt.ReadFrom(src)
	//}
	for {
		if srcc, ok := src.(net.Conn); ok {
			// Best effort
			_ = srcc.SetReadDeadline(time.Now().Add(15 * time.Minute))
		}
		nr, err := src.Read(buf)
		log.Debugf("read %v/%v", nr, err)
		if nr > 0 { // before dealing with the read error
			nw, ew := dst.Write(buf[0:nr])
			log.Debugf("write %v/%v", nw, ew)
			if f, ok := dst.(http.Flusher); ok {
				f.Flush()
			}
			if nr != nw { // Should not happen
				ew = io.ErrShortWrite
			}
			if ew != nil {
				return
			}
		}
		if err != nil {
			// read is already closed - we need to close out
			_ = closeWriter(dst)
			return
		}
	}
}

// CloseWriter is one of possible interfaces implemented by Out to send a FIN, without closing
// the input. Some writers only do this when Close is called.
type CloseWriter interface {
	CloseWrite() error
}

func closeWriter(dst io.Writer) error {
	if cw, ok := dst.(CloseWriter); ok {
		return cw.CloseWrite()
	}
	if c, ok := dst.(io.Closer); ok {
		return c.Close()
	}
	if rw, ok := dst.(http.ResponseWriter); ok {
		// Server side HTTP stream. For client side, FIN can be sent by closing the pipe (or
		// request body). For server, the FIN will be sent when the handler returns - but
		// this only happen after request is completed and body has been read. If server wants
		// to send FIN first - while still reading the body - we are in trouble.

		// That means HTTP2 TCP servers provide no way to send a FIN from server, without
		// having the request fully read.

		// This works for H2 with the current library - but very tricky, if not set as trailer.
		rw.Header().Set("X-Close", "0")
		rw.(http.Flusher).Flush()
		return nil
	}
	log.Infof("Server out not Closer nor CloseWriter nor ResponseWriter: %v", dst)
	return nil
}
