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

/*
Code in this class is mostly a copy from https://github.com/golang/protobuf/blob/master/protoc-gen-go/generator/generator.go
TODO:
-  Handle name collision. When multiple files in the FileDescriptorSet have the same name, the protoc-gen-go do name
   substitution before printing the package name. Our generated code that might reference those packages should also
   use the substituted name that protoc-gen-go would use to name that package.
-  Handle go_package option. Currently the name of the package is computed just based on the file path.
-  Handle Oneofs. The protoc-gen-go have special logic (removed from this file, but present in the protoc-gen-go).
   Our generated interface code might also need that special logic if the Instance object wants to reference a
   oneof from the generated mytemplate.pb.go
-  Handle Extensions. Should we allow Instance message to have extension fields. Extensions have are a complete
   different handling in protoc-gen-go. So far I am what it means for our generated code.
-  Accept all the other parameters that protoc-gen-go takes in our generator. Our codegen should use those
   parameters for it's codegen and pass to the protoc-gen-go plugin (invoking another protoc process).
*/

package descriptor

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

// FileDescriptorSetParser parses the FileDescriptorSetProto and creates an intermediate object that is used to
// create Model struct.
type FileDescriptorSetParser struct {
	typeNameToObject  map[string]Object // Key is a fully-qualified name in input syntax.
	Param             map[string]string // Command-line parameters.
	PackageImportPath string            // Go import path of the package we're generating code for
	ImportMap         map[string]string // Mapping from .proto file name to import path

	Pkg map[string]string // The names under which we import support packages

	PackageName    string                     // What we're calling ourselves.
	AllFiles       []*FileDescriptor          // All files in the tree
	allFilesByName map[string]*FileDescriptor // All files by filename.
}

type common struct {
	FileDesc *descriptor.FileDescriptorProto // File this object comes from.
}

// FileDescriptor wraps a FileDescriptorProto.
type FileDescriptor struct {
	*descriptor.FileDescriptorProto
	Desc []*Descriptor     // All the messages defined in this file.
	enum []*EnumDescriptor // All the enums defined in this file.

	// lineNumbers stored as a map of path (comma-separated integers) to string.
	lineNumbers map[string]string

	// Comments, stored as a map of path (comma-separated integers) to the comment.
	comments map[string]*descriptor.SourceCodeInfo_Location

	Proto3 bool // whether to generate proto3 code for this file
}

// Descriptor wraps a DescriptorProto.
type Descriptor struct {
	common
	*descriptor.DescriptorProto
	parent   *Descriptor       // The containing message, if any.
	nested   []*Descriptor     // Inner messages, if any.
	enums    []*EnumDescriptor // Inner enums, if any.
	typename []string          // Cached typename vector.
	Path     string            // The SourceCodeInfo path as comma-separated integers.
}

// EnumDescriptor wraps a EnumDescriptorProto.
type EnumDescriptor struct {
	common
	*descriptor.EnumDescriptorProto
	parent   *Descriptor // The containing message, if any.
	typename []string    // Cached typename vector.
	path     string      // The SourceCodeInfo path as comma-separated integers.
}

// Object is a shared interface implemented by EnumDescriptor and Descriptor
type Object interface {
	PackageName() string // The name we use in our output (a_b_c), possibly renamed for uniqueness.
	TypeName() []string
	File() *descriptor.FileDescriptorProto
}

// Enums return a complete list of enums in the file descriptor set
func (g *FileDescriptorSetParser) Enums() map[string]*descriptor.EnumDescriptorProto {
	enums := make(map[string]*descriptor.EnumDescriptorProto)
	for n, o := range g.typeNameToObject {
		if v, ok := o.(*EnumDescriptor); ok {
			enums[n] = v.EnumDescriptorProto
		}
	}
	return enums
}

// Messages return a complete list of messages in the file descriptor set
func (g *FileDescriptorSetParser) Messages() map[string]*descriptor.DescriptorProto {
	msgs := make(map[string]*descriptor.DescriptorProto)
	for n, o := range g.typeNameToObject {
		if v, ok := o.(*Descriptor); ok {
			msgs[n] = v.DescriptorProto
		}
	}
	return msgs
}

// CreateFileDescriptorSetParser builds a FileDescriptorSetParser instance.
func CreateFileDescriptorSetParser(fds *descriptor.FileDescriptorSet, importMap map[string]string, packageImportPath string) *FileDescriptorSetParser {
	parser := &FileDescriptorSetParser{ImportMap: importMap, PackageImportPath: packageImportPath}
	parser.WrapTypes(fds)
	parser.BuildTypeNameMap()

	return parser
}

// WrapTypes creates wrapper types for messages, enumse and file inside the FileDescriptorSet.
func (g *FileDescriptorSetParser) WrapTypes(fds *descriptor.FileDescriptorSet) {
	g.AllFiles = make([]*FileDescriptor, 0, len(fds.File))
	g.allFilesByName = make(map[string]*FileDescriptor, len(g.AllFiles))
	for _, f := range fds.File {
		g.wrapFileDescriptor(f)

	}
}

func (g *FileDescriptorSetParser) wrapFileDescriptor(f *descriptor.FileDescriptorProto) {
	if _, ok := g.allFilesByName[f.GetName()]; !ok {
		// We must wrap the descriptors before we wrap the enums
		descs := wrapDescriptors(f)
		g.buildNestedDescriptors(descs)
		enums := wrapEnumDescriptors(f, descs)
		g.buildNestedEnums(descs, enums)
		fd := &FileDescriptor{
			FileDescriptorProto: f,
			Desc:                descs,
			enum:                enums,
			Proto3:              fileIsProto3(f),
		}
		extractCommentsAndLineNumbers(fd)
		g.AllFiles = append(g.AllFiles, fd)
		g.allFilesByName[f.GetName()] = fd
	}
}

func wrapDescriptors(file *descriptor.FileDescriptorProto) []*Descriptor {
	sl := make([]*Descriptor, 0, len(file.MessageType)+10)
	for i, desc := range file.MessageType {
		sl = wrapThisDescriptor(sl, desc, nil, file, i)
	}
	return sl
}

func wrapThisDescriptor(sl []*Descriptor, desc *descriptor.DescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) []*Descriptor {
	sl = append(sl, newDescriptor(desc, parent, file, index))
	me := sl[len(sl)-1]
	for i, nested := range desc.NestedType {
		sl = wrapThisDescriptor(sl, nested, me, file, i)
	}
	return sl
}

func wrapEnumDescriptors(file *descriptor.FileDescriptorProto, descs []*Descriptor) []*EnumDescriptor {
	sl := make([]*EnumDescriptor, 0, len(file.EnumType)+10)
	// Top-level enums.
	for i, enum := range file.EnumType {
		sl = append(sl, newEnumDescriptor(enum, nil, file, i))
	}
	// Enums within messages. Enums within embedded messages appear in the outer-most message.
	for _, nested := range descs {
		for i, enum := range nested.EnumType {
			sl = append(sl, newEnumDescriptor(enum, nested, file, i))
		}
	}
	return sl
}

// Descriptor methods ..

func newDescriptor(desc *descriptor.DescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) *Descriptor {
	d := &Descriptor{
		common:          common{file},
		DescriptorProto: desc,
		parent:          parent,
	}

	if parent == nil {
		d.Path = fmt.Sprintf("%s,%d", MessagePath, index)
	} else {
		d.Path = fmt.Sprintf("%s,%s,%d", parent.Path, MessageMessagePath, index)
	}
	return d
}

func (g *FileDescriptorSetParser) buildNestedDescriptors(descs []*Descriptor) {
	for _, desc := range descs {
		if len(desc.NestedType) != 0 {
			for _, nest := range descs {
				if nest.parent == desc {
					desc.nested = append(desc.nested, nest)
				}
			}
		}
	}
}

// TypeName returns a cached typename vector.
func (d *Descriptor) TypeName() []string {
	if d.typename != nil {
		return d.typename
	}
	n := 0
	for parent := d; parent != nil; parent = parent.parent {
		n++
	}
	s := make([]string, n)
	for parent := d; parent != nil; parent = parent.parent {
		n--
		s[n] = parent.GetName()
	}
	d.typename = s
	return s
}

// Enum methods ..

func newEnumDescriptor(desc *descriptor.EnumDescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) *EnumDescriptor {
	ed := &EnumDescriptor{
		common:              common{file},
		EnumDescriptorProto: desc,
		parent:              parent,
	}
	if parent == nil {
		ed.path = fmt.Sprintf("%s,%d", EnumPath, index)
	} else {
		ed.path = fmt.Sprintf("%s,%s,%d", parent.Path, MessageEnumPath, index)
	}
	return ed
}

func (g *FileDescriptorSetParser) buildNestedEnums(descs []*Descriptor, enums []*EnumDescriptor) {
	for _, desc := range descs {
		if len(desc.EnumType) != 0 {
			for _, enum := range enums {
				if enum.parent == desc {
					desc.enums = append(desc.enums, enum)
				}
			}
		}
	}
}

// TypeName returns a cached typename vector.
func (e *EnumDescriptor) TypeName() (s []string) {
	if e.typename != nil {
		return e.typename
	}
	name := e.GetName()
	if e.parent == nil {
		s = make([]string, 1)
	} else {
		pname := e.parent.TypeName()
		s = make([]string, len(pname)+1)
		copy(s, pname)
	}
	s[len(s)-1] = name
	e.typename = s
	return s
}

// FileDescriptorSetParser methods

func (g *FileDescriptorSetParser) fail(msgs ...string) {
	s := strings.Join(msgs, " ")
	fmt.Fprintln(os.Stderr, "model_generator: error:", s)
	os.Exit(1)
}

// TypeName returns a full name for the underlying Object type.
func (g *FileDescriptorSetParser) TypeName(obj Object) string {
	return g.DefaultPackageName(obj) + camelCaseSlice(obj.TypeName())
}

// DefaultPackageName returns a full packagen name for the underlying Object type.
func (g *FileDescriptorSetParser) DefaultPackageName(obj Object) string {
	// TODO if the protoc is not executed with --include_imports, this
	// is guaranteed to throw NPE.
	// Handle it.
	if obj == nil {
		return ""
	}
	pkg := obj.PackageName()
	if pkg == g.PackageName {
		return ""
	}
	return pkg + "."
}

// ObjectNamed returns the descriptor for the message or enum with that name.
func (g *FileDescriptorSetParser) ObjectNamed(typeName string) Object {
	o, ok := g.typeNameToObject[typeName]
	if !ok {
		g.fail("can't find object with type", typeName)
	}

	return o
}

func extractCommentsAndLineNumbers(file *FileDescriptor) {
	file.lineNumbers = make(map[string]string)
	file.comments = make(map[string]*descriptor.SourceCodeInfo_Location)
	for _, loc := range file.GetSourceCodeInfo().GetLocation() {
		var p []string
		for _, n := range loc.Path {
			p = append(p, strconv.Itoa(int(n)))
		}
		file.lineNumbers[strings.Join(p, ",")] = strconv.Itoa(int(loc.Span[0]) + 1)
		if loc.LeadingComments != nil {
			file.comments[strings.Join(p, ",")] = loc
		}
	}
}

// GetComment returns the comment associated with the element at a given path.
func (d *FileDescriptor) GetComment(path string) string {
	if loc, ok := d.comments[path]; ok {
		text := strings.TrimSuffix(loc.GetLeadingComments(), "\n")
		var buffer bytes.Buffer
		for _, line := range strings.Split(text, "\n") {
			buffer.WriteString("// " + strings.TrimPrefix(line, " ") + "\n")
		}
		return strings.TrimSuffix(buffer.String(), "\n")
	}
	return ""
}

// GetLineNumber returns line number for the path.
func (d *FileDescriptor) GetLineNumber(path string) string {
	if l, ok := d.lineNumbers[path]; ok {
		return l
	}

	return ""
}

// GetPathForField returns the path associated with the field
func GetPathForField(desc *Descriptor, index int) string {
	return fmt.Sprintf("%s,%s,%d", desc.Path, MessageFieldPath, index)
}

// BuildTypeNameMap creates a map of type name to the wrapper Object associated with it.
func (g *FileDescriptorSetParser) BuildTypeNameMap() {
	g.typeNameToObject = make(map[string]Object)
	for _, f := range g.AllFiles {
		// The names in this loop are defined by the proto world, not us, so the
		// package name may be empty.  If so, the dotted package name of X will
		// be ".X"; otherwise it will be ".pkg.X".
		dottedPkg := "." + f.GetPackage()
		if dottedPkg != "." {
			dottedPkg += "."
		}
		for _, enum := range f.enum {
			name := dottedPkg + dottedSlice(enum.TypeName())
			g.typeNameToObject[name] = enum
		}
		for _, desc := range f.Desc {
			name := dottedPkg + dottedSlice(desc.TypeName())
			g.typeNameToObject[name] = desc
		}
	}
}

// common struct methods

// PackageName returns the Go package name for the file the common object belongs to.
func (c *common) PackageName() string {
	f := c.FileDesc
	return GoPackageName(f.GetPackage())
}

func (c *common) File() *descriptor.FileDescriptorProto { return c.FileDesc }

// helper methods

func dottedSlice(elem []string) string { return strings.Join(elem, ".") }

// Is c an ASCII lower-case letter?
func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}

// Is c an ASCII digit?
func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

// CamelCase converts the string into camel case string
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	t := make([]byte, 0, 32)
	i := 0
	if s[0] == '_' {
		// Need a capital letter; drop the '_'.
		t = append(t, 'X')
		i++
	}
	// Invariant: if the next letter is lower case, it must be converted
	// to upper case.
	// That is, we process a word at a time, where words are marked by _ or
	// upper case letter. Digits are treated as words.
	for ; i < len(s); i++ {
		c := s[i]
		if c == '_' && i+1 < len(s) && isASCIILower(s[i+1]) {
			continue // Skip the underscore in s.
		}
		if isASCIIDigit(c) {
			t = append(t, c)
			continue
		}
		// Assume we have a letter now - if not, it's a bogus identifier.
		// The next word is a sequence of characters that must start upper case.
		if isASCIILower(c) {
			c ^= ' ' // Make it a capital letter.
		}
		t = append(t, c) // Guaranteed not lower case.
		// Accept lower case sequence that follows.
		for i+1 < len(s) && isASCIILower(s[i+1]) {
			i++
			t = append(t, s[i])
		}
	}
	return string(t)
}

func camelCaseSlice(elem []string) string { return CamelCase(strings.Join(elem, "_")) }

func fileIsProto3(file *descriptor.FileDescriptorProto) bool {
	return file.GetSyntax() == "proto3"
}

// badToUnderscore is the mapping function used to generate Go names from package names,
// which can be dotted in the input .proto file.  It replaces non-identifier characters such as
// dot or dash with underscore.
func badToUnderscore(r rune) rune {
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
		return r
	}
	return '_'
}

// GoPackageName converts proto package name into go package name.
func GoPackageName(pkg string) string {
	return strings.Map(badToUnderscore, pkg)
}

const (
	// SyntaxPath is location path number for syntax statement
	SyntaxPath = "12" // syntax
	// PackagePath is location path number for package statement
	PackagePath = "2" // syntax
	// MessagePath is location path number for message statement
	MessagePath = "4" // message_type
	// EnumPath is location path number for enum statement
	EnumPath = "5" // enum_type
	// MessageFieldPath is location path number for message field statement
	MessageFieldPath = "2" // field
	// MessageMessagePath is location path number for sub-message statement
	MessageMessagePath = "3" // nested_type
	// MessageEnumPath is location path number for sub-enum statement
	MessageEnumPath = "4" // enum_type
)
