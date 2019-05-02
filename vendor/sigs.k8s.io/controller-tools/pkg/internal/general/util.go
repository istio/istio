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

package general

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// isGoFile filters files from parsing.
func isGoFile(f os.FileInfo) bool {
	// ignore non-Go or Go test files
	name := f.Name()
	return !f.IsDir() &&
		!strings.HasPrefix(name, ".") &&
		!strings.HasSuffix(name, "_test.go") &&
		strings.HasSuffix(name, ".go")
}

// GetAnnotation extracts the annotation from comment text.
// It will return "foo" for comment "+kubebuilder:webhook:foo" .
func GetAnnotation(c, name string) string {
	prefix := fmt.Sprintf("+%s:", name)
	if strings.HasPrefix(c, prefix) {
		return strings.TrimPrefix(c, prefix)
	}
	return ""
}

// ParseKV parses key-value string formatted as "foo=bar" and returns key and value.
func ParseKV(s string) (key, value string, err error) {
	kv := strings.Split(s, "=")
	if len(kv) != 2 {
		err = fmt.Errorf("invalid key value pair")
		return key, value, err
	}
	key, value = kv[0], kv[1]
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		value = value[1 : len(value)-1]
	}
	return key, value, err
}

// ParseDir parses the Go files under given directory and parses the annotation by
// invoking the parseFn function on each comment group (multi-lines comments).
// TODO(droot): extend it to multiple dirs
func ParseDir(dir string, parseFn func(string) error) error {
	fset := token.NewFileSet()

	err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if !isGoFile(info) {
				// TODO(droot): enable this output based on verbose flag
				// fmt.Println("skipping non-go file", path)
				return nil
			}
			return ParseFile(fset, path, nil, parseFn)
		})
	return err
}

// ParseFile parses given filename or content src and parses annotations by
// invoking the parseFn function on each comment group (multi-lines comments).
func ParseFile(fset *token.FileSet, filename string, src interface{}, parseFn func(string) error) error {
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		fmt.Printf("error from parse.ParseFile: %v", err)
		return err
	}

	// using commentMaps here because it sanitizes the comment text by removing
	// comment markers, compresses newlines etc.
	cmap := ast.NewCommentMap(fset, f, f.Comments)

	for _, commentGroup := range cmap.Comments() {
		err = parseFn(commentGroup.Text())
		if err != nil {
			fmt.Print("error when parsing annotation")
			return err
		}
	}
	return nil
}
