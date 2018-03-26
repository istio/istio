// Copyright 2018 Istio Authors.
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

package registry

import (
	"fmt"

	"encoding/base64"
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	multierror "github.com/hashicorp/go-multierror"

	adapter "istio.io/api/mixer/adapter/model/v1beta1"
	tmpl "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/pkg/log"
)

// TemplateMetadata contains info about a template
type TemplateMetadata struct {
	Name          string
	FileDescSet   *descriptor.FileDescriptorSet
	FileDescProto *descriptor.FileDescriptorProto
}

// AdapterMetadata contains info about an adapter
type AdapterMetadata struct {
	Info               *adapter.Info
	SupportedTemplates []string
}

// Registry to find metadata about templates and adapters
type Registry interface {
	GetAdapter(name string) *AdapterMetadata
	GetTemplate(name string) *TemplateMetadata
}

// registry to ingest templates and adapters. It is single threaded.
type registry struct {
	adapters  map[string]*AdapterMetadata
	templates map[string]*TemplateMetadata
}

// New creates a `Registry` from given adapter infos.
// Note: For adding built-in templates that are not associated with any adapters, supply the `Info` object with
// only `templates`, leaving other fields to default empty.
func New(infos []adapter.Info) (Registry, error) {
	r := &registry{make(map[string]*AdapterMetadata), make(map[string]*TemplateMetadata)}
	var resultErr error
	log.Debugf("registering %#v", infos)

	for _, info := range infos {
		if old := r.GetAdapter(info.Name); old != nil {
			// duplicate entry found
			resultErr = multierror.Append(resultErr,
				fmt.Errorf("duplicate registration for adapter '%s' : new = %v old = %v", info.Name, info, old))
			continue
		}

		tmplNames, err := r.ingestTemplates(info.Templates)
		if err != nil {
			resultErr = multierror.Append(resultErr, err)
			continue
		}

		// empty adapter name means just the template needs to be ingested.
		if info.Name != "" {
			r.adapters[info.Name] = &AdapterMetadata{Info: &info, SupportedTemplates: tmplNames}
		}
	}

	if resultErr != nil {
		log.Error(resultErr.Error())
	}

	return r, resultErr
}

// GetAdapterInfo returns a AdapterMetadata for a adapter with the given name.
func (r *registry) GetAdapter(name string) *AdapterMetadata {
	if bi, found := r.adapters[name]; found {
		return bi
	}
	return nil
}

// GetTemplate returns a TemplateMetadata for a template with the given name.
func (r *registry) GetTemplate(name string) *TemplateMetadata {
	if bi, found := r.templates[name]; found {
		return bi
	}

	return nil
}

func (r *registry) ingestTemplates(tmpls []string) ([]string, error) {
	var resultErr error
	templates := make([]*TemplateMetadata, 0, len(tmpls))
	for _, tmpl := range tmpls {
		tmplMeta, err := r.createTemplateMetadata(tmpl)
		if err != nil {
			resultErr = multierror.Append(resultErr, err)
			// accumulate all errors
			continue
		}
		templates = append(templates, tmplMeta)
	}
	if resultErr != nil {
		return nil, resultErr
	}

	// No errors, so we can now safely ingest the templates
	tmplNames := make([]string, 0, len(templates))
	for _, tmplMeta := range templates {
		r.templates[tmplMeta.Name] = tmplMeta
		tmplNames = append(tmplNames, tmplMeta.Name)
	}
	return tmplNames, resultErr
}

func (r *registry) createTemplateMetadata(base64Tmpl string) (*TemplateMetadata, error) {
	var bytes []byte
	var err error

	reader := strings.NewReader(base64Tmpl)
	decoder := base64.NewDecoder(base64.StdEncoding, reader)

	if bytes, err = ioutil.ReadAll(decoder); err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	if err = proto.Unmarshal(bytes, fds); err != nil {
		return nil, err
	}
	var tmplDesc *descriptor.FileDescriptorProto
	if tmplDesc, err = getTmplFileDesc(fds.File); err != nil {
		return nil, err
	}

	// TODO: Fix this; Using package name to recreate template name is a hack. Ideally we should put the template name
	// as an option when generating the augmented proto (we have already computed it during codegen).
	var lastSeg string
	if lastSeg, err = getLastSegment(strings.TrimSpace(tmplDesc.GetPackage())); err != nil {
		return nil, err
	}
	tmplName := strings.ToLower(lastSeg)

	// TODO: if given template is already registered, pick the one that is superset. For now just overwrite; last one wins.
	if old := r.GetTemplate(tmplName); old != nil {
		// duplicate entry found TODO: how can we make this error better ??
		log.Errorf("duplicate registration for template '%s'; picking the last one", tmplName)
	}

	return &TemplateMetadata{Name: tmplName,
		FileDescProto: tmplDesc,
		FileDescSet:   fds}, nil
}

// Find the file that has the options TemplateVariety. There should only be one such file.
func getTmplFileDesc(fds []*descriptor.FileDescriptorProto) (*descriptor.FileDescriptorProto, error) {
	var templateDescriptorProto *descriptor.FileDescriptorProto
	for _, fd := range fds {
		if fd.GetOptions() == nil || !proto.HasExtension(fd.GetOptions(), tmpl.E_TemplateVariety) {
			continue
		}
		if templateDescriptorProto != nil {
			return nil, fmt.Errorf(
				"proto files %s and %s, both have the option %s. Only one proto file is allowed with this options",
				fd.GetName(), templateDescriptorProto.GetName(), tmpl.E_TemplateVariety.Name)
		}
		templateDescriptorProto = fd
	}

	if templateDescriptorProto == nil {
		return nil, fmt.Errorf("there has to be one proto file that has the extension %s", tmpl.E_TemplateVariety.Name)
	}

	return templateDescriptorProto, nil
}

var pkgLaskSegRegex = regexp.MustCompile("^[a-zA-Z]+$")

func getLastSegment(pkg string) (string, error) {
	if pkg != "" {
		segs := strings.Split(pkg, ".")
		last := segs[len(segs)-1]
		if pkgLaskSegRegex.MatchString(last) {
			return last, nil
		}
	}
	return "", fmt.Errorf("the last segment of package name '%s' must match the regex '%s'", pkg, "^[a-zA-Z]+$")
}
