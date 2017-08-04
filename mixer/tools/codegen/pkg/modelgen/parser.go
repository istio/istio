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
-  Handle Extensions. Should we allow Constructor message to have extension fields. Extensions have are a complete
   different handling in protoc-gen-go. So far I am what it means for our generated code.
-  Accept all the other parameters that protoc-gen-go takes in our generator. Our codegen should use those
   parameters for it's codegen and pass to the protoc-gen-go plugin (invoking another protoc process).
*/

package modelgen

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

	packageName    string                     // What we're calling ourselves.
	allFiles       []*FileDescriptor          // All files in the tree
	allFilesByName map[string]*FileDescriptor // All files by filename.
}

type common struct {
	file *descriptor.FileDescriptorProto // File this object comes from.
}

// FileDescriptor wraps a FileDescriptorProto.
type FileDescriptor struct {
	*descriptor.FileDescriptorProto
	desc []*Descriptor     // All the messages defined in this file.
	enum []*EnumDescriptor // All the enums defined in this file.

	// lineNumbers stored as a map of path (comma-separated integers) to string.
	lineNumbers map[string]string

	// Comments, stored as a map of path (comma-separated integers) to the comment.
	comments map[string]*descriptor.SourceCodeInfo_Location

	proto3 bool // whether to generate proto3 code for this file
}

// Descriptor wraps a DescriptorProto.
type Descriptor struct {
	common
	*descriptor.DescriptorProto
	parent   *Descriptor       // The containing message, if any.
	nested   []*Descriptor     // Inner messages, if any.
	enums    []*EnumDescriptor // Inner enums, if any.
	typename []string          // Cached typename vector.
	path     string            // The SourceCodeInfo path as comma-separated integers.
	group    bool
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
	Path() string
}

// CreateFileDescriptorSetParser builds a FileDescriptorSetParser instance.
func CreateFileDescriptorSetParser(fds *descriptor.FileDescriptorSet, importMap map[string]string, packageImportPath string) (*FileDescriptorSetParser, error) {
	parser := &FileDescriptorSetParser{ImportMap: importMap, PackageImportPath: packageImportPath}
	parser.WrapTypes(fds)
	parser.BuildTypeNameMap()
	return parser, nil
}

// WrapTypes creates wrapper types for messages, enumse and file inside the FileDescriptorSet.
func (g *FileDescriptorSetParser) WrapTypes(fds *descriptor.FileDescriptorSet) {
	g.allFiles = make([]*FileDescriptor, 0, len(fds.File))
	g.allFilesByName = make(map[string]*FileDescriptor, len(g.allFiles))
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
			desc:                descs,
			enum:                enums,
			proto3:              fileIsProto3(f),
		}
		extractCommentsAndLineNumbers(fd)
		g.allFiles = append(g.allFiles, fd)
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
		d.path = fmt.Sprintf("%s,%d", messagePath, index)
	} else {
		d.path = fmt.Sprintf("%s,%s,%d", parent.path, messageMessagePath, index)
	}

	// The only way to distinguish a group from a message is whether
	// the containing message has a TYPE_GROUP field that matches.
	if parent != nil {
		parts := d.TypeName()
		if file.Package != nil {
			parts = append([]string{*file.Package}, parts...)
		}
		exp := "." + strings.Join(parts, ".")
		for _, field := range parent.Field {
			if field.GetType() == descriptor.FieldDescriptorProto_TYPE_GROUP && field.GetTypeName() == exp {
				d.group = true
				break
			}
		}
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
			if len(desc.nested) != len(desc.NestedType) {
				g.fail("internal error: nesting failure for", desc.GetName())
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

// Path returns the path string associated with this Descriptor object.
func (d *Descriptor) Path() string {
	return d.path
}

// Enum methods ..

func newEnumDescriptor(desc *descriptor.EnumDescriptorProto, parent *Descriptor, file *descriptor.FileDescriptorProto, index int) *EnumDescriptor {
	ed := &EnumDescriptor{
		common:              common{file},
		EnumDescriptorProto: desc,
		parent:              parent,
	}
	if parent == nil {
		ed.path = fmt.Sprintf("%s,%d", enumPath, index)
	} else {
		ed.path = fmt.Sprintf("%s,%s,%d", parent.path, messageEnumPath, index)
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
			if len(desc.enums) != len(desc.EnumType) {
				g.fail("internal error: enum nesting failure for", desc.GetName())
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

// Path returns the path string associated with this EnumDescriptor object.
func (e *EnumDescriptor) Path() string {
	return e.path
}

// FileDescriptorSetParser methods

func (g *FileDescriptorSetParser) fail(msgs ...string) {
	s := strings.Join(msgs, " ")
	fmt.Fprintln(os.Stderr, "model_generator: error:", s)
	os.Exit(1)
}

const (
	sINT64     = "int64"
	sFLOAT64   = "float64"
	sINT32     = "int32"
	sFLOAT32   = "float32"
	sBOOL      = "bool"
	sSTRING    = "string"
	sUINT64    = "uint64"
	sUINT32    = "uint32"
	sBYTEARRAY = "[]byte"
)

// goType returns a Go type name for a FieldDescriptorProto.
func (g *FileDescriptorSetParser) goType(message *descriptor.DescriptorProto, field *descriptor.FieldDescriptorProto) (typ string) {
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		typ = sFLOAT64
	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		typ = sFLOAT32
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		typ = sINT64
	case descriptor.FieldDescriptorProto_TYPE_UINT64:
		typ = sUINT64
	case descriptor.FieldDescriptorProto_TYPE_INT32:
		typ = sINT32
	case descriptor.FieldDescriptorProto_TYPE_UINT32:
		typ = sUINT32
	case descriptor.FieldDescriptorProto_TYPE_FIXED64:
		typ = sUINT64
	case descriptor.FieldDescriptorProto_TYPE_FIXED32:
		typ = sUINT32
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		typ = sBOOL
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		typ = sSTRING
	case descriptor.FieldDescriptorProto_TYPE_GROUP:
		// TODO : What needs to be done in this case?
		g.fail(fmt.Sprintf("unsupported field type %s for field %s", descriptor.FieldDescriptorProto_TYPE_GROUP.String(), field.GetName()))
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		desc := g.ObjectNamed(field.GetTypeName())
		typ = "*" + g.TypeName(desc)

		if d, ok := desc.(*Descriptor); ok && d.GetOptions().GetMapEntry() {
			keyField, valField := d.Field[0], d.Field[1]
			keyType := g.goType(d.DescriptorProto, keyField)
			valType := g.goType(d.DescriptorProto, valField)

			keyType = strings.TrimPrefix(keyType, "*")
			switch *valField.Type {
			case descriptor.FieldDescriptorProto_TYPE_ENUM:
				valType = strings.TrimPrefix(valType, "*")

			case descriptor.FieldDescriptorProto_TYPE_MESSAGE:

			default:
				valType = strings.TrimPrefix(valType, "*")
			}

			typ = fmt.Sprintf("map[%s]%s", keyType, valType)
			return
		}
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		typ = sBYTEARRAY
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		desc := g.ObjectNamed(field.GetTypeName())
		typ = g.TypeName(desc)
	case descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		typ = sINT32
	case descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		typ = sINT64
	case descriptor.FieldDescriptorProto_TYPE_SINT32:
		typ = sINT32
	case descriptor.FieldDescriptorProto_TYPE_SINT64:
		typ = sINT64
	default:
		g.fail("unknown type for", field.GetName())

	}
	if isRepeated(field) {
		typ = "[]" + typ
	} else if message != nil {
		return
	} else if field.OneofIndex != nil && message != nil {
		g.fail("oneof not supported ", field.GetName())
		return
	} else if needsStar(*field.Type) {
		typ = "*" + typ
	}
	return
}

// protoType returns a Proto type name for a Field's DescriptorProto.
// We only support primitives that can be represented as ValueTypes,ValueType itself, or map<string, ValueType>.
var supportedTypes = "string, int64, double, bool, " + fullProtoNameOfValueTypeEnum + ", " + fmt.Sprintf("map<string, %s>", fullProtoNameOfValueTypeEnum)

func (g *FileDescriptorSetParser) protoType(field *descriptor.FieldDescriptorProto) (typ string, err error) {
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		typ = "string"
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		typ = "int64"
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		typ = "double"
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		typ = "bool"
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		typ = field.GetTypeName()[1:]
		if typ != fullProtoNameOfValueTypeEnum {
			return "", fmt.Errorf("unsupported type for field '%s'. Supported types are '%s'", field.GetName(), supportedTypes)
		}
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		desc := g.ObjectNamed(field.GetTypeName())
		if d, ok := desc.(*Descriptor); ok && d.GetOptions().GetMapEntry() {
			keyField, valField := d.Field[0], d.Field[1]

			keyType, err := g.protoType(keyField)
			if err != nil {
				return "", err
			}
			valType, err := g.protoType(valField)
			if err != nil {
				return "", err
			}

			if keyType != "string" || valType != fullProtoNameOfValueTypeEnum {
				return "", fmt.Errorf("unsupported type for field '%s'. Supported types are '%s'", field.GetName(), supportedTypes)
			}

			typ = fmt.Sprintf("map<%s, %s>", keyType, valType)
			return typ, nil
		}
		return "", fmt.Errorf("unsupported type for field '%s'. Supported types are '%s'", field.GetName(), supportedTypes)
	default:
		return "", fmt.Errorf("unsupported type for field '%s'. Supported types are '%s'", field.GetName(), supportedTypes)
	}
	return typ, nil
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
	if pkg == g.packageName {
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

func (d *FileDescriptor) getComment(path string) string {
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

func (d *FileDescriptor) getLineNumber(path string) string {
	if l, ok := d.lineNumbers[path]; ok {
		return l
	}

	return ""
}

func getPathForField(desc *Descriptor, index int) string {
	return fmt.Sprintf("%s,%s,%d", desc.path, messageFieldPath, index)
}

// BuildTypeNameMap creates a map of type name to the wrapper Object associated with it.
func (g *FileDescriptorSetParser) BuildTypeNameMap() {
	g.typeNameToObject = make(map[string]Object)
	for _, f := range g.allFiles {
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
		for _, desc := range f.desc {
			name := dottedPkg + dottedSlice(desc.TypeName())
			g.typeNameToObject[name] = desc
		}
	}
}

// common struct methods

// PackageName returns the Go package name for the file the common object belongs to.
func (c *common) PackageName() string {
	f := c.file
	return goPackageName(f.GetPackage())
}

func (c *common) File() *descriptor.FileDescriptorProto { return c.file }

// helper methods

func dottedSlice(elem []string) string { return strings.Join(elem, ".") }

func isRepeated(field *descriptor.FieldDescriptorProto) bool {
	return field.Label != nil && *field.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
}

func needsStar(typ descriptor.FieldDescriptorProto_Type) bool {
	switch typ {
	case descriptor.FieldDescriptorProto_TYPE_GROUP:
		return false
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		return false
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		return false
	}
	return true
}

// Is c an ASCII lower-case letter?
func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}

// Is c an ASCII digit?
func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func camelCase(s string) string {
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

func camelCaseSlice(elem []string) string { return camelCase(strings.Join(elem, "_")) }

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

func goPackageName(pkg string) string {
	return strings.Map(badToUnderscore, pkg)
}

const (
	syntaxPath         = "12" // syntax
	packagePath        = "2"  // syntax
	messagePath        = "4"  // message_type
	enumPath           = "5"  // enum_type
	messageFieldPath   = "2"  // field
	messageMessagePath = "3"  // nested_type
	messageEnumPath    = "4"  // enum_type
)
