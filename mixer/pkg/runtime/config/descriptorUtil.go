// Copyright Istio Authors.
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

package config

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	tmpl "istio.io/api/mixer/adapter/model/v1beta1"
)

const adapterCfgMsgName = "Params"

// GetTmplDescriptor find a template descriptor
func GetTmplDescriptor(base64Tmpl string) (*descriptor.FileDescriptorSet, *descriptor.FileDescriptorProto, string, tmpl.TemplateVariety, error) {
	fds, err := decodeFds(base64Tmpl)
	tmplVariety := tmpl.TEMPLATE_VARIETY_REPORT
	if err != nil {
		return nil, nil, "", tmplVariety, err
	}

	var tmplDescProto *descriptor.FileDescriptorProto
	for _, fd := range fds.File {
		if fd.GetOptions() == nil {
			continue
		}
		var variety interface{}
		if variety, err = proto.GetExtension(fd.GetOptions(), tmpl.E_TemplateVariety); err == nil {
			tmplVariety = *(variety.(*tmpl.TemplateVariety))
			if tmplDescProto != nil {
				return nil, nil, "", tmplVariety, fmt.Errorf(
					"proto files %s and %s, both have the option %s. Only one proto file is allowed with this options",
					fd.GetName(), tmplDescProto.GetName(), tmpl.E_TemplateVariety.Name)
			}
			tmplDescProto = fd
		}
	}

	if tmplDescProto == nil {
		return nil, nil, "", tmplVariety, fmt.Errorf("there has to be one proto file that has the extension %s", tmpl.E_TemplateVariety.Name)
	}

	var nameExt interface{}
	if nameExt, err = proto.GetExtension(tmplDescProto.GetOptions(), tmpl.E_TemplateName); err != nil {
		return nil, nil, "", tmplVariety, fmt.Errorf(
			"proto files %s is missing required template_name option", tmplDescProto.GetName())
	}

	if err = validateTmplName(*(nameExt.(*string))); err != nil {
		return nil, nil, "", tmplVariety, err
	}

	return fds, tmplDescProto, *(nameExt.(*string)), tmplVariety, nil
}

// GetAdapterCfgDescriptor find an adapter configuration descriptor
func GetAdapterCfgDescriptor(base64Tmpl string) (*descriptor.FileDescriptorSet, *descriptor.FileDescriptorProto, error) {
	if base64Tmpl == "" {
		// no cfg is allowed
		return &descriptor.FileDescriptorSet{}, &descriptor.FileDescriptorProto{}, nil
	}

	fds, err := decodeFds(base64Tmpl)
	if err != nil {
		return nil, nil, err
	}

	var cfgDesc *descriptor.FileDescriptorProto
	for _, fd := range fds.File {
		for _, msg := range fd.GetMessageType() {
			if msg.GetName() == adapterCfgMsgName {
				cfgDesc = fd
			}
		}
	}

	if cfgDesc == nil {
		return nil, nil, fmt.Errorf("cannot find message named '%s' in the adapter configuration descriptor", adapterCfgMsgName)
	}

	return fds, cfgDesc, nil
}

var pkgLaskSegRegex = regexp.MustCompile("^[a-zA-Z]+$")

func validateTmplName(name string) error {
	if !pkgLaskSegRegex.MatchString(name) {
		return fmt.Errorf("the template name '%s' must match the regex '%s'", name, "^[a-zA-Z]+$")
	}
	return nil
}

func decodeFds(base64Fds string) (*descriptor.FileDescriptorSet, error) {
	var err error
	var bytes []byte

	reader := strings.NewReader(base64Fds)
	decoder := base64.NewDecoder(base64.StdEncoding, reader)

	if bytes, err = ioutil.ReadAll(decoder); err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	if err = proto.Unmarshal(bytes, fds); err != nil {
		return nil, err
	}

	return fds, nil
}
