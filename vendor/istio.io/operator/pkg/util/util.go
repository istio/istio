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

package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var (
	letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
)

type FileFilter func(fileName string) bool

// RandomString returns a random string of length n.
func RandomString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// PrettyJSON returns a pretty printed version of the JSON string b.
func PrettyJSON(b []byte) []byte {
	var out bytes.Buffer
	err := json.Indent(&out, b, "", "  ")
	if err != nil {
		return []byte(fmt.Sprint(err))
	}
	return out.Bytes()
}

// StringBoolMapToSlice creates and returns a slice of all the map keys with true.
func StringBoolMapToSlice(m map[string]bool) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			s = append(s, k)
		}
	}
	return s
}

// ReadFiles reads a directory recursively or reads single file and filters the results.
// It returns a concatenated output of all matching files' content.
func ReadFiles(dirName string, filter FileFilter) (string, error) {
	var fileList []string
	err := filepath.Walk(dirName, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !filter(path) {
			return nil
		}
		fileList = append(fileList, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, file := range fileList {
		a, err := ioutil.ReadFile(file)
		if err != nil {
			return "", err
		}
		if _, err := sb.WriteString(string(a) + "\n"); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// ParseValue parses string into a value
func ParseValue(valueStr string) interface{} {
	var value interface{}
	if v, err := strconv.Atoi(valueStr); err == nil {
		value = v
	} else if v, err := strconv.ParseFloat(valueStr, 64); err == nil {
		value = v
	} else if v, err := strconv.ParseBool(valueStr); err == nil {
		value = v
	} else {
		value = valueStr
	}
	return value
}
