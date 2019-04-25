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

package parse

import (
	"bufio"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/args"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/types"
	"sigs.k8s.io/controller-tools/pkg/internal/codegen"
)

// APIs is the information of a collection of API
type APIs struct {
	context         *generator.Context
	arguments       *args.GeneratorArgs
	Domain          string
	VersionedPkgs   sets.String
	UnversionedPkgs sets.String
	APIsPkg         string
	APIsPkgRaw      *types.Package
	GroupNames      sets.String

	APIs        *codegen.APIs
	Controllers []codegen.Controller

	ByGroupKindVersion    map[string]map[string]map[string]*codegen.APIResource
	ByGroupVersionKind    map[string]map[string]map[string]*codegen.APIResource
	SubByGroupVersionKind map[string]map[string]map[string]*types.Type
	Groups                map[string]types.Package
	Rules                 []rbacv1.PolicyRule
	Informers             map[v1.GroupVersionKind]bool
}

// NewAPIs returns a new APIs instance with given context.
func NewAPIs(context *generator.Context, arguments *args.GeneratorArgs, domain, apisPkg string) *APIs {
	b := &APIs{
		context:   context,
		arguments: arguments,
		Domain:    domain,
		APIsPkg:   apisPkg,
	}
	b.parsePackages()
	b.parseGroupNames()
	b.parseIndex()
	b.parseAPIs()
	b.parseCRDs()
	if len(b.Domain) == 0 {
		b.parseDomain()
	}
	return b
}

// parseGroupNames initializes b.GroupNames with the set of all groups
func (b *APIs) parseGroupNames() {
	b.GroupNames = sets.String{}
	for p := range b.UnversionedPkgs {
		pkg := b.context.Universe[p]
		if pkg == nil {
			// If the input had no Go files, for example.
			continue
		}
		b.GroupNames.Insert(filepath.Base(p))
	}
}

// parsePackages parses out the sets of Versioned, Unversioned packages and identifies the root Apis package.
func (b *APIs) parsePackages() {
	b.VersionedPkgs = sets.NewString()
	b.UnversionedPkgs = sets.NewString()
	for _, o := range b.context.Order {
		if IsAPIResource(o) {
			versioned := o.Name.Package
			b.VersionedPkgs.Insert(versioned)

			unversioned := filepath.Dir(versioned)
			b.UnversionedPkgs.Insert(unversioned)
		}
	}
}

// parseDomain parses the domain from the apis/doc.go file comment "// +domain=YOUR_DOMAIN".
func (b *APIs) parseDomain() {
	pkg := b.context.Universe[b.APIsPkg]
	if pkg == nil {
		// If the input had no Go files, for example.
		panic(errors.Errorf("Missing apis package."))
	}
	comments := Comments(pkg.Comments)
	b.Domain = comments.getTag("domain", "=")
	if len(b.Domain) == 0 {
		b.Domain = parseDomainFromFiles(b.context.Inputs)
		if len(b.Domain) == 0 {
			panic("Could not find string matching // +domain=.+ in apis/doc.go")
		}
	}
}

func parseDomainFromFiles(paths []string) string {
	var domain string
	for _, path := range paths {
		if strings.HasSuffix(path, "pkg/apis") {
			filePath := strings.Join([]string{build.Default.GOPATH, "src", path, "doc.go"}, "/")
			lines := []string{}

			file, err := os.Open(filePath)
			if err != nil {
				log.Fatal(err)
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "//") {
					lines = append(lines, strings.Replace(scanner.Text(), "// ", "", 1))
				}
			}
			if err := scanner.Err(); err != nil {
				log.Fatal(err)
			}

			comments := Comments(lines)
			domain = comments.getTag("domain", "=")
			break
		}
	}
	return domain
}
