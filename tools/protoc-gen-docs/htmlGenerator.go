// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this currentFile except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"

	blackfriday "gopkg.in/russross/blackfriday.v2"
)

type outputMode int

const (
	htmlPage outputMode = iota
	htmlFragment
	jekyllHTML
)

type htmlGenerator struct {
	buffer      bytes.Buffer
	model       *model
	mode        outputMode
	genWarnings bool
	emitYAML    bool

	// transient state as individual files are processed
	currentFile *fileDescriptor
	grouping    bool
}

func newHTMLGenerator(model *model, mode outputMode, genWarnings bool, emitYAML bool) *htmlGenerator {
	return &htmlGenerator{
		model:       model,
		mode:        mode,
		genWarnings: genWarnings,
		emitYAML:    emitYAML,
	}
}

func (g *htmlGenerator) generateOutput(filesToGen map[*fileDescriptor]bool) *plugin.CodeGeneratorResponse {
	// process each package, we produce one output file per package
	response := plugin.CodeGeneratorResponse{}

	filesPerPackage := make(map[*packageDescriptor][]*fileDescriptor)
	for _, pkg := range g.model.packages {
		for _, file := range pkg.files {
			if filesToGen[file] {
				slice := filesPerPackage[pkg]
				filesPerPackage[pkg] = append(slice, file)
			}
		}
	}

	// we now have the minimum set of proto files to include in each output
	// package doc file. We augment this here with the set of referenced proto files
	// which don't have location information. So if we don't know where a particular
	// proto's doc lives on the web, we include its description locally.
	for _, pkg := range g.model.packages {
		slice := filesPerPackage[pkg]

		for _, file := range slice {
			filesPerPackage[pkg] = includeUnsituatedDependencies(slice, file.dependencies)
		}
	}

	for _, pkg := range g.model.packages {
		files := filesPerPackage[pkg]
		if len(files) > 0 {
			rf := g.generateFile(pkg, files)
			response.File = append(response.File, &rf)
		}
	}

	return &response
}

func includeUnsituatedDependencies(slice []*fileDescriptor, files []*fileDescriptor) []*fileDescriptor {
	for _, file := range files {
		if file.parent.location == "" {
			slice = append(slice, file)
			slice = includeUnsituatedDependencies(slice, file.dependencies)
		}
	}

	return slice
}

func (g *htmlGenerator) generateFile(pkg *packageDescriptor, files []*fileDescriptor) plugin.CodeGeneratorResponse_File {
	g.buffer.Reset()
	g.generateFileHeader(pkg)

	var enums []string
	var messages []string
	var services []string

	enumMap := make(map[string]*enumDescriptor)
	messageMap := make(map[string]*messageDescriptor)
	serviceMap := make(map[string]*serviceDescriptor)

	for _, file := range files {
		g.currentFile = file

		for _, enum := range file.allEnums {
			absName := g.absoluteTypeName(enum)
			known := wellKnownTypes[absName]
			if known != "" {
				continue
			}

			name := g.relativeTypeName(enum)
			enums = append(enums, name)
			enumMap[name] = enum
		}
		sort.Strings(enums)

		for _, msg := range file.allMessages {
			// Don't generate virtual messages for maps.
			if msg.GetOptions().GetMapEntry() {
				continue
			}

			absName := g.absoluteTypeName(msg)
			known := wellKnownTypes[absName]
			if known != "" {
				continue
			}

			name := g.relativeTypeName(msg)
			messages = append(messages, name)
			messageMap[name] = msg
		}
		sort.Strings(messages)

		for _, svc := range file.services {
			name := *svc.Name
			services = append(services, name)
			serviceMap[name] = svc
		}
		sort.Strings(services)
	}

	numKinds := 0
	if len(enums) > 0 {
		numKinds++
	}
	if len(messages) > 0 {
		numKinds++
	}
	if len(services) > 0 {
		numKinds++
	}

	// if there's more than one kind of thing, divide the output in groups
	g.grouping = numKinds > 1

	if len(enums) > 0 {
		if g.grouping {
			g.emit("<h2 id='Enumerations'>Enumerations</h2>")
		}

		for _, name := range enums {
			e := enumMap[name]
			g.currentFile = e.fileDesc()
			g.generateEnum(e)
		}
	}

	if len(messages) > 0 {
		if g.grouping {
			g.emit("<h2 id='Messages'>Messages</h2>")
		}

		for _, name := range messages {
			message := messageMap[name]
			g.currentFile = message.fileDesc()
			g.generateMessage(message)
		}
	}

	if len(services) > 0 {
		if g.grouping {
			g.emit("<h2 id='Services'>Services</h2>")
		}

		for _, name := range services {
			service := serviceMap[name]
			g.currentFile = service.fileDesc()
			g.generateService(service)
		}
	}
	g.generateFileFooter()

	return plugin.CodeGeneratorResponse_File{
		Name:    proto.String(pkg.name + ".pb.html"),
		Content: proto.String(g.buffer.String()),
	}
}

func (g *htmlGenerator) generateFileHeader(pkg *packageDescriptor) {
	if g.mode == jekyllHTML {
		g.emit("---")

		if pkg.title != "" {
			g.emit("title: ", pkg.title)
		} else {
			g.emit("title: ", pkg.name)
		}

		if pkg.overview != "" {
			g.emit("overview: ", pkg.overview)
		}

		if pkg.location != "" {
			g.emit("location: ", pkg.location)
		}

		g.emit("type: reference")
		g.emit("layout: docs")
		g.emit("---")
		g.emit("<!-- Generated by protoc-gen-docs -->")
	} else if g.mode == htmlPage {
		g.emit("<!DOCTYPE html>")
		g.emit("<html itemscope itemtype='https://schema.org/WebPage'>")
		g.emit("<!-- Generated by protoc-gen-docs -->")
		g.emit("<head>")
		g.emit("<meta charset='utf-8'>")
		g.emit("<meta name='viewport' content='width=device-width, initial-scale=1, shrink-to-fit=no'>")

		if pkg.title != "" {
			g.emit("<meta name='title' content='", pkg.title, "'>")
			g.emit("<meta name='og:title' content='", pkg.title, "'>")
			g.emit("<title>", pkg.title, "</title>")
		}

		if pkg.overview != "" {
			g.emit("<meta name='description' content='", pkg.overview, "'>")
			g.emit("<meta name='og:description' content='", pkg.overview, "'>")
		}

		g.emit(htmlStyle)

		g.emit("</head>")
		g.emit("<body>")
		if pkg.title != "" {
			g.emit("<h1>", pkg.title, "</h1>")
		}
	} else if g.mode == htmlFragment {
		g.emit("<!-- Generated by protoc-gen-docs -->")
		if pkg.title != "" {
			g.emit("<h1>", pkg.title, "</h1>")
		}
	}

	count := 0
	for _, loc := range pkg.loc {
		if loc.LeadingComments != nil {
			g.generateComment(loc, "")
			count++
		}
	}

	if count == 0 {
		g.warn(nil, "no comment for package %s", pkg.name)
	}
}

func (g *htmlGenerator) generateFileFooter() {
	if g.mode == htmlPage {
		g.emit("</body>")
		g.emit("</html>")
	}
}

func (g *htmlGenerator) generateSectionHeading(name string) {
	if g.grouping {
		g.emit("<h3 id='", name, "'>", name, "</h3>")
	} else {
		g.emit("<h2 id='", name, "'>", name, "</h2>")
	}
	g.emit("<section>")
}

func (g *htmlGenerator) generateSectionTrailing() {
	g.emit("</section>")
}

func (g *htmlGenerator) generateMessage(message *messageDescriptor) {
	g.generateSectionHeading(g.relativeTypeName(message))
	g.generateComment(message.loc, message.GetName())

	if len(message.fields) > 0 {
		g.emit("<table>")
		g.emit("<tr>")
		g.emit("<th>Field</th>")
		g.emit("<th>Type</th>")
		g.emit("<th>Description</th>")
		g.emit("</tr>")

		var oneof int32 = -1
		for _, field := range message.fields {
			if hasNotImplementedAnnotation(field.loc) {
				continue
			}

			fieldName := camelCase(*field.Name)
			fieldTypeName := g.fieldTypeName(field)

			if field.OneofIndex != nil {
				class := ""
				if field.Options != nil && field.Options.GetDeprecated() {
					class = "deprecated "
				}

				if *field.OneofIndex != oneof {
					class = class + "oneof oneof-start"
					oneof = *field.OneofIndex
				} else {
					class = class + "oneof"
				}
				g.emit("<tr class='", class, "'>")
			} else {
				if field.Options != nil && field.Options.GetDeprecated() {
					g.emit("<tr class='deprecated' title='Deprecated'>")
				} else {
					g.emit("<tr>")
				}
			}

			g.emit("<td><code>", fieldName, "</code></td>")
			g.emit("<td><code>", g.linkify(field.typ, fieldTypeName), "</code></td>")
			g.emit("<td>")

			g.generateComment(field.loc, field.GetName())

			g.emit("</td>")
			g.emit("</tr>")
		}
		g.emit("</table>")

		/*
			if g.emitYAML {
				g.emit("<br />")

				g.emit("<table>")
				g.emit("<tr><th>YAML</th></tr>")
				g.emit("<tr><td>")
				g.emit("<pre><code class='language-yaml'>")

				for _, field := range message.fields {

					fieldTypeName := g.fieldYAMLTypeName(field)
					g.emit(camelCase(field.GetName()), ": ", fieldTypeName)
				}

				g.emit("</code></pre>")
				g.emit("</td></tr>")
				g.emit("</table>")
			}
		*/
	}

	g.generateSectionTrailing()
}

func (g *htmlGenerator) generateEnum(enum *enumDescriptor) {
	g.generateSectionHeading(g.relativeTypeName(enum))
	g.generateComment(enum.loc, enum.GetName())

	if len(enum.values) > 0 {
		g.emit("<table>")
		g.emit("<tr>")
		g.emit("<th>Name</th>")
		g.emit("<th>Description</th>")
		g.emit("</tr>")

		for _, v := range enum.values {
			if hasNotImplementedAnnotation(v.loc) {
				continue
			}

			name := *v.Name

			if v.Options != nil && v.Options.GetDeprecated() {
				g.emit("<tr class='deprecated' title='Deprecated'>")
			} else {
				g.emit("<tr>")
			}
			g.emit("<td><code>", name, "</code></td>")
			g.emit("<td>")

			g.generateComment(v.loc, name)

			g.emit("</td>")
			g.emit("</tr>")
		}
		g.emit("</table>")
	}

	g.generateSectionTrailing()
}

func (g *htmlGenerator) generateService(service *serviceDescriptor) {
	g.generateSectionHeading(service.GetName())
	g.generateComment(service.loc, service.GetName())

	for _, method := range service.methods {
		if hasNotImplementedAnnotation(method.loc) {
			continue
		}

		if method.Options != nil && method.Options.GetDeprecated() {
			g.emit("<pre class='deprecated' title='Deprecated'><code class='language-proto'>rpc ",
				method.GetName(), "(", g.relativeTypeName(method.input), ") returns (", g.relativeTypeName(method.output), ")")
		} else {
			g.emit("<pre><code class='language-proto'>rpc ",
				method.GetName(), "(", g.relativeTypeName(method.input), ") returns (", g.relativeTypeName(method.output), ")")
		}
		g.emit("</code></pre>")

		g.generateComment(method.loc, method.GetName())
	}

	g.generateSectionTrailing()
}

// emit prints the arguments to the generated output.
func (g *htmlGenerator) emit(str ...string) {
	for _, s := range str {
		g.buffer.WriteString(s)
	}
	g.buffer.WriteByte('\n')
}

var typeLinkPattern = regexp.MustCompile("\\[.*\\]\\[.*\\]")

func hasNotImplementedAnnotation(loc *descriptor.SourceCodeInfo_Location) bool {
	com := loc.GetLeadingComments()
	if com == "" {
		com = loc.GetTrailingComments()
		if com == "" {
			return false
		}
	}

	return strings.Contains(com, "$hide_from_docs") || strings.Contains(com, "[#not-implemented-hide:]")
}

func (g *htmlGenerator) generateComment(loc *descriptor.SourceCodeInfo_Location, name string) {
	com := loc.GetLeadingComments()
	if com == "" {
		com = loc.GetTrailingComments()
		if com == "" {
			g.warn(loc, "no comment found for %s", name)
			return
		}
	}

	text := strings.TrimSuffix(com, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 {
		// Based on the amount of spacing at the start of the first line,
		// remove that many bytes at the beginning of every line in the comment.
		// This is so we don't inject extra spaces in any preformatted blocks included
		// in the comments
		pad := 0
		for i, ch := range lines[0] {
			if !unicode.IsSpace(ch) {
				pad = i
				break
			}
		}

		for i := 0; i < len(lines); i++ {
			l := lines[i]
			if len(l) > pad {
				lines[i] = l[pad:]
			}
		}

		// now, adjust any headers included in the comment to correspond to the right
		// level, based on the heading level of the surrounding content
		for i := 0; i < len(lines); i++ {
			l := lines[i]
			if strings.HasPrefix(l, "#") {
				if g.grouping {
					lines[i] = "##" + l
				} else {
					lines[i] = "#" + l
				}
			}
		}

		// find any type links of the form [name][type] and turn
		// them into normal HTML links
		for i := 0; i < len(lines); i++ {

			lines[i] = typeLinkPattern.ReplaceAllStringFunc(lines[i], func(match string) string {
				end := 0
				for match[end] != ']' {
					end++
				}

				linkName := match[1:end]
				typeName := match[end+2 : len(match)-1]

				if o, ok := g.model.allCoreDescByName["."+typeName]; ok {
					return g.linkify(o, linkName)
				}

				if l, ok := wellKnownTypes[typeName]; ok {
					return "<a href='" + l + "'>" + linkName + "</a>"
				}

				return "*" + linkName + "*"
			})
		}
	}

	text = strings.Join(lines, "\n")

	// turn the comment from markdown into HTML
	result := blackfriday.Run([]byte(text))

	g.buffer.Write(result)
	g.buffer.WriteByte('\n')
}

// well-known types whose documentation we can refer to
var wellKnownTypes = map[string]string{
	"google.protobuf.Duration":               "https://developers.google.com/protocol-buffers/docs/reference/google.protobuf#google.protobuf.Duration",
	"google.protobuf.Timestamp":              "https://developers.google.com/protocol-buffers/docs/reference/google.protobuf#google.protobuf.Timestamp",
	"google.api.MetricDescriptor.MetricKind": "https://cloud.google.com/logging/docs/reference/v2/rest/v2/projects.metrics#metrickind",
	"google.api.MetricDescriptor.ValueType":  "https://cloud.google.com/logging/docs/reference/v2/rest/v2/projects.metrics#valuetype",
	"google.api.MetricDescriptor":            "https://cloud.google.com/logging/docs/reference/v2/rest/v2/projects.metrics#metricdescriptor",
}

func (g *htmlGenerator) linkify(o coreDesc, name string) string {
	if o == nil {
		return name
	}

	if msg, ok := o.(*messageDescriptor); ok && msg.GetOptions().GetMapEntry() {
		return name
	}

	known := wellKnownTypes[g.absoluteTypeName(o)]
	if known != "" {
		return "<a href='" + known + "'>" + name + "</a>"
	}

	loc := o.fileDesc().parent.location
	fragment := "#" + dottedName(o)

	if loc != "" && loc != g.currentFile.parent.location {
		return "<a href='" + loc + fragment + "'>" + name + "</a>"
	}

	return "<a href='" + fragment + "'>" + name + "</a>"
}

func (g *htmlGenerator) warn(loc *descriptor.SourceCodeInfo_Location, format string, args ...interface{}) {
	if g.genWarnings {
		place := ""
		if loc != nil {
			place = fmt.Sprintf("%s:%d:%d:", g.currentFile.GetName(), loc.Span[0], loc.Span[1])
		}

		fmt.Fprintf(os.Stderr, place+" "+format+"\n", args...)
	}
}

func (g *htmlGenerator) relativeTypeName(desc coreDesc) string {
	typeName := dottedName(desc)
	if desc.fileDesc().parent == g.currentFile.parent {
		return typeName
	}

	return desc.fileDesc().parent.name + "." + typeName
}

func (g *htmlGenerator) absoluteTypeName(desc coreDesc) string {
	typeName := dottedName(desc)
	return desc.fileDesc().parent.name + "." + typeName
}

func (g *htmlGenerator) fieldTypeName(field *fieldDescriptor) string {
	name := "n/a"
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		name = "double"

	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		name = "float"

	case descriptor.FieldDescriptorProto_TYPE_INT32, descriptor.FieldDescriptorProto_TYPE_SINT32, descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		name = "int32"

	case descriptor.FieldDescriptorProto_TYPE_INT64, descriptor.FieldDescriptorProto_TYPE_SINT64, descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		name = "int64"

	case descriptor.FieldDescriptorProto_TYPE_UINT64, descriptor.FieldDescriptorProto_TYPE_FIXED64:
		name = "uint64"

	case descriptor.FieldDescriptorProto_TYPE_UINT32, descriptor.FieldDescriptorProto_TYPE_FIXED32:
		name = "uint32"

	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		name = "bool"

	case descriptor.FieldDescriptorProto_TYPE_STRING:
		name = "string"

	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		msg := field.typ.(*messageDescriptor)
		if msg.GetOptions().GetMapEntry() {
			keyType := g.fieldTypeName(msg.fields[0])
			valType := g.linkify(msg.fields[1].typ, g.fieldTypeName(msg.fields[1]))
			return "map&lt;" + keyType + ", " + valType + "&gt;"
		}
		name = g.relativeTypeName(field.typ)

	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		name = "bytes"

	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		name = g.relativeTypeName(field.typ)
	}

	if field.isRepeated() {
		name = name + "[]"
	}

	if field.OneofIndex != nil {
		name = name + " (oneof)"
	}

	return name
}

/* TODO
func (g *htmlGenerator) fieldYAMLTypeName(field *fieldDescriptor) string {
	name := "n/a"
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		name = "double"

	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		name = "float"

	case descriptor.FieldDescriptorProto_TYPE_INT32, descriptor.FieldDescriptorProto_TYPE_SINT32, descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		name = "int32"

	case descriptor.FieldDescriptorProto_TYPE_INT64, descriptor.FieldDescriptorProto_TYPE_SINT64, descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		name = "int64"

	case descriptor.FieldDescriptorProto_TYPE_UINT64, descriptor.FieldDescriptorProto_TYPE_FIXED64:
		name = "uint64"

	case descriptor.FieldDescriptorProto_TYPE_UINT32, descriptor.FieldDescriptorProto_TYPE_FIXED32:
		name = "uint32"

	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		name = "bool"

	case descriptor.FieldDescriptorProto_TYPE_STRING:
		name = "string"

	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		msg := field.typ.(*messageDescriptor)
		if msg.GetOptions().GetMapEntry() {
			keyType := g.fieldTypeName(msg.fields[0])
			valType := g.linkify(msg.fields[1].typ, g.fieldTypeName(msg.fields[1]))
			return "map&lt;" + keyType + ", " + valType + "&gt;"
		}
		name = g.relativeTypeName(field.typ)

	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		name = "bytes"

	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		name = "enum(" + g.relativeTypeName(field.typ) + ")"
	}

	return name
}
*/

// camelCase returns the camelCased name.
func camelCase(s string) string {
	b := bytes.Buffer{}
	nextUpper := false
	for _, ch := range s {
		if ch == '_' {
			nextUpper = true
		} else {
			if nextUpper {
				nextUpper = false
				ch = unicode.ToUpper(ch)
			}
			b.WriteRune(ch)
		}
	}

	return b.String()
}

var htmlStyle = `
<style>
    html {
        overflow-y: scroll;
        position: relative;
        min-height: 100%
    }

    body {
        font-family: "Roboto", "Helvetica Neue", Helvetica, Arial, sans-serif;
        color: #535f61
    }

    a {
        color: #466BB0;
        text-decoration: none;
        font-weight: 500
    }

    a:hover, a:focus {
        color: #8ba3d1;
        text-decoration: none;
        font-weight: 500
    }

    a.disabled {
        color: #ccc;
        text-decoration: none;
        font-weight: 500
    }

    table, th, td {
        border: 1px solid #849396;
        padding: .3em
    }

	tr.oneof>td {
		border-bottom: 1px dashed #849396;
		border-top: 1px dashed #849396;
	}

    table {
        border-collapse: collapse
    }

    th {
        color: #fff;
        background-color: #286AC7;
        font-weight: normal
    }

    p {
        font-size: 1rem;
        line-height: 1.5;
        margin: .25em 0
    }

	table p:first-of-type {
		margin-top: 0
	}

	table p:last-of-type {
		margin-bottom: 0
	}

    @media (min-width: 768px) {
        p {
            margin: 1.5em 0
        }
    }

    li, dt, dd {
        font-size: 1rem;
        line-height: 1.5;
        margin: .25em
    }

    ol, ul, dl {
        list-style: initial;
        font-size: 1rem;
        margin: 0 1.5em;
        padding: 0
    }

    li p, dt p, dd p {
        margin: .4em 0
    }

    ol {
        list-style: decimal
    }

    h1, h2, h3, h4, h5, h6 {
        border: 0;
        font-weight: normal
    }

    h1 {
        font-size: 2.5rem;
        color: #286AC7;
        margin: 30px 0
    }

    h2 {
        font-size: 2rem;
        color: #2E2E2E;
        margin-bottom: 20px;
        margin-top: 30px;
        padding-bottom: 10px;
        border-bottom: 1px;
        border-color: #737373;
        border-style: solid
    }

    h3 {
        font-size: 1.85rem;
        font-weight: 500;
        color: #404040;
        letter-spacing: 1px;
        margin-bottom: 20px;
        margin-top: 30px
    }

    h4 {
        font-size: 1.85rem;
        font-weight: 500;
        margin: 30px 0 20px;
        color: #404040
    }

    em {
        font-style: italic
    }

    strong {
        font-weight: bold
    }

    blockquote {
        display: block;
        margin: 1em 3em;
        background-color: #f8f8f8
    }

	section {
		padding-left: 2em;
	}

	code {
		color: red;
	}

	.deprecated {
		background: silver;
	}
</style>
`
