// Copyright 2017 Istio Authors
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

// An example implementation of a client.

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	flag.Parse()

	if len(os.Args) < 2 {
		log.Fatal("Must supply at least one URL")
	}

	url := os.Args[1]
	fmt.Printf("Url=%s\n", url)

	var headerKey, headerVal string
	if len(os.Args) > 2 {
		headerKey = os.Args[2]
		if len(os.Args) > 3 {
			headerVal = os.Args[3]
		} else {
			headerVal = "junk"
		}
		fmt.Printf("Header=%s:%s\n", headerKey, headerVal)
	}

	client := http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Println(err.Error())
		return
	}
	if headerKey != "" {
		req.Header.Add(headerKey, headerVal)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Println(err.Error())
		return
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	if err != nil {
		log.Println(err.Error())
	}

	err = resp.Body.Close()
	if err != nil {
		log.Println(err.Error())
	}
}
