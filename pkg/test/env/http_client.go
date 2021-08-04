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

package env

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	// HTTP client time out.
	httpTimeOut = 5 * time.Second

	// Maximum number of ping the server to wait.
	maxAttempts = 30
)

// HTTPGet send GET
func HTTPGet(url string) (code int, respBody string, err error) {
	log.Println("HTTP GET", url)
	client := &http.Client{Timeout: httpTimeOut}
	resp, err := client.Get(url)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	respBody = string(body)
	code = resp.StatusCode
	return code, respBody, nil
}

// WaitForHTTPServer waits for a HTTP server
func WaitForHTTPServer(url string) error {
	for i := 0; i < maxAttempts; i++ {
		log.Println("Pinging URL: ", url)
		code, _, err := HTTPGet(url)
		if err == nil && code == http.StatusOK {
			log.Println("Server is up and running...")
			return nil
		}
		log.Println("Will wait 200ms and try again.")
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for server startup")
}

// WaitForPort waits for a TCP port
func WaitForPort(port uint16) {
	serverPort := fmt.Sprintf("localhost:%v", port)
	for i := 0; i < maxAttempts; i++ {
		log.Println("Pinging port: ", serverPort)
		_, err := net.Dial("tcp", serverPort)
		if err == nil {
			log.Println("The port is up and running...")
			return
		}
		log.Println("Wait 200ms and try again.")
		time.Sleep(200 * time.Millisecond)
	}
	log.Println("Give up the wait, continue the test...")
}

// IsPortUsed checks if a port is used
func IsPortUsed(port uint16) bool {
	serverPort := fmt.Sprintf("localhost:%v", port)
	_, err := net.DialTimeout("tcp", serverPort, 100*time.Millisecond)
	return err == nil
}
