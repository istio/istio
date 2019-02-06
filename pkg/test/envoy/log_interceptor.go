//  Copyright 2019 Istio Authors
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

package envoy

import (
	"io"
	"os"
	"strings"
	"time"
)

type logHandlerFunc func(logLine string)
type errorHandlerFunc func(err error)

// logInterceptor intercepts envoy logs, calling back a handler with each log line received.
type logInterceptor struct {
	target       io.Writer
	pipeReader   io.Reader
	pipeWriter   io.Writer
	handler      logHandlerFunc
	errorHandler errorHandlerFunc
	stopCh       chan struct{}
}

func newLogInterceptor(target io.Writer, handler logHandlerFunc, errorHandler errorHandlerFunc) (*logInterceptor, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	return &logInterceptor{
		target:       target,
		handler:      handler,
		errorHandler: errorHandler,
		pipeReader:   pr,
		pipeWriter:   pw,
		stopCh:       make(chan struct{}),
	}, nil
}

func (i *logInterceptor) Writer() io.Writer {
	return i.pipeWriter

}
func (i *logInterceptor) start() {
	go func() {
		var content string
		buf := make([]byte, 1024)

		// Read from the pipe periodically.
		readTicker := time.NewTicker(10 * time.Millisecond)
		defer readTicker.Stop()

		for {
			select {
			case <-i.stopCh:
				return
			case <-readTicker.C:
				// Read whatever is currently available in the log.
				for {
					// Read a chunk from the pipe
					n, err := i.pipeReader.Read(buf)
					if err != nil {
						i.errorHandler(err)
						return
					}

					// Append to the content.
					chunk := buf[:n]
					content += string(chunk)

					// Forward the content to target file.
					if _, err := i.target.Write(chunk); err != nil {
						i.errorHandler(err)
						return
					}

					if n < len(buf) {
						// Nothing currently available.
						break
					}
				}

				lines := strings.Split(content, "\n")

				// Notify the interceptor of all full lines since the last notification.
				for _, line := range lines[0 : len(lines)-1] {
					i.handler(line)
				}

				// Set the content to the data remaining after the last carriage return.
				content = lines[len(lines)-1]
			}
		}
	}()
}

func (i *logInterceptor) stop() {
	close(i.stopCh)
}
