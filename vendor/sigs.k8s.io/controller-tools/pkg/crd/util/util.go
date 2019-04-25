/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"bufio"
	"fmt"
	gobuild "go/build"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IsGoSrcPath validate if given path is of path $GOPATH/src.
func IsGoSrcPath(filePath string) bool {
	for _, gopath := range getGoPaths() {
		goSrc := path.Join(gopath, "src")
		if filePath == goSrc {
			return true
		}
	}

	return false
}

// IsUnderGoSrcPath validate if given path is under path $GOPATH/src.
func IsUnderGoSrcPath(filePath string) bool {
	for _, gopath := range getGoPaths() {
		goSrc := path.Join(gopath, "src")
		if strings.HasPrefix(filepath.Dir(filePath), goSrc) {
			return true
		}
	}

	return false
}

// DirToGoPkg returns the Gopkg for the given directory if it exists
// under a GOPATH otherwise returns error. For example,
// /Users/x/go/src/github.com/y/z ==> github.com/y/z
func DirToGoPkg(dir string) (pkg string, err error) {
	goPaths := getGoPaths()
	for _, gopath := range goPaths {
		goSrc := path.Join(gopath, "src")
		if !strings.HasPrefix(dir, goSrc) {
			continue
		}
		pkg, err := filepath.Rel(goSrc, dir)
		if err == nil {
			return pkg, err
		}
	}

	return "", fmt.Errorf("dir '%s' does not exist under any GOPATH %v", dir, goPaths)
}

func getGoPaths() []string {
	gopaths := os.Getenv("GOPATH")
	if len(gopaths) == 0 {
		gopaths = gobuild.Default.GOPATH
	}
	return filepath.SplitList(gopaths)
}

// PathHasProjectFile validate if PROJECT file exists under the path.
func PathHasProjectFile(filePath string) bool {
	if _, err := os.Stat(path.Join(filePath, "PROJECT")); os.IsNotExist(err) {
		return false
	}

	return true
}

// GetDomainFromProject get domain information from the PROJECT file under the path.
func GetDomainFromProject(rootPath string) string {
	var domain string

	file, err := os.Open(path.Join(rootPath, "PROJECT"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "domain:") {
			domainInfo := strings.Split(scanner.Text(), ":")
			if len(domainInfo) != 2 {
				log.Fatalf("Unexpected domain info: %s", scanner.Text())
			}
			domain = strings.Replace(domainInfo[1], " ", "", -1)
			break
		}
	}

	return domain
}
