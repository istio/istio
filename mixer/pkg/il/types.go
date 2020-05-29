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

package il

// Type represents a core type in the il system.
type Type uint32

const (
	// Unknown represents a type that is unknown.
	Unknown Type = iota

	// Void represents the void type.
	Void

	// String represents the string type.
	String

	// Integer represents a 64-bit signed integer.
	Integer

	// Double represents a 64-bit signed floating point number.
	Double

	// Bool represents a boolean value.
	Bool

	// Duration represents a time.Duration value
	Duration

	// Interface represents a generic interface{} value
	Interface
)

var typeNames = map[Type]string{
	Unknown:   "unknown",
	Void:      "void",
	String:    "string",
	Integer:   "integer",
	Double:    "double",
	Bool:      "bool",
	Duration:  "duration",
	Interface: "interface",
}

var typesByName = map[string]Type{
	"void":      Void,
	"string":    String,
	"integer":   Integer,
	"double":    Double,
	"bool":      Bool,
	"duration":  Duration,
	"interface": Interface,
}

func (t Type) String() string {
	return typeNames[t]
}

// GetType returns the type with the given name, if it exists.
func GetType(name string) (Type, bool) {
	t, f := typesByName[name]
	return t, f
}
