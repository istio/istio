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

	resp, err := http.Get(url)
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
