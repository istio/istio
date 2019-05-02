package inflect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gobuffalo/envy"
)

// Name is a string that represents the "name" of a thing, like an app, model, etc...
type Name string

// Title version of a name. ie. "foo_bar" => "Foo Bar"
func (n Name) Title() string {
	x := strings.Split(string(n), "/")
	for i, s := range x {
		x[i] = Titleize(s)
	}

	return strings.Join(x, " ")
}

// Underscore version of a name. ie. "FooBar" => "foo_bar"
func (n Name) Underscore() string {
	w := string(n)
	if strings.ToUpper(w) == w {
		return strings.ToLower(w)
	}
	return Underscore(w)
}

// Plural version of a name
func (n Name) Plural() string {
	return Pluralize(string(n))
}

// Singular version of a name
func (n Name) Singular() string {
	return Singularize(string(n))
}

// Camel version of a name
func (n Name) Camel() string {
	c := Camelize(string(n))
	if strings.HasSuffix(c, "Id") {
		c = strings.TrimSuffix(c, "Id")
		c += "ID"
	}
	return c
}

// Model version of a name. ie. "user" => "User"
func (n Name) Model() string {
	x := strings.Split(string(n), "/")
	for i, s := range x {
		x[i] = Camelize(Singularize(s))
	}

	return strings.Join(x, "")
}

// Resource version of a name
func (n Name) Resource() string {
	name := n.Underscore()
	x := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '/'
	})

	for i, w := range x {
		if i == len(x)-1 {
			x[i] = Camelize(Pluralize(strings.ToLower(w)))
			continue
		}

		x[i] = Camelize(w)
	}

	return strings.Join(x, "")
}

// ModelPlural version of a name. ie. "user" => "Users"
func (n Name) ModelPlural() string {
	return Camelize(Pluralize(n.Model()))
}

// File version of a name
func (n Name) File() string {
	return Underscore(Camelize(string(n)))
}

// Table version of a name
func (n Name) Table() string {
	return Underscore(Pluralize(string(n)))
}

// UnderSingular version of a name
func (n Name) UnderSingular() string {
	return Underscore(Singularize(string(n)))
}

// PluralCamel version of a name
func (n Name) PluralCamel() string {
	return Pluralize(Camelize(string(n)))
}

// PluralUnder version of a name
func (n Name) PluralUnder() string {
	return Pluralize(Underscore(string(n)))
}

// URL version of a name
func (n Name) URL() string {
	return n.PluralUnder()
}

// CamelSingular version of a name
func (n Name) CamelSingular() string {
	return Camelize(Singularize(string(n)))
}

// VarCaseSingular version of a name. ie. "FooBar" => "fooBar"
func (n Name) VarCaseSingular() string {
	return CamelizeDownFirst(Singularize(Underscore(n.Resource())))
}

// VarCasePlural version of a name. ie. "FooBar" => "fooBar"
func (n Name) VarCasePlural() string {
	return CamelizeDownFirst(n.Resource())
}

// Lower case version of a string
func (n Name) Lower() string {
	return strings.ToLower(string(n))
}

// ParamID returns foo_bar_id
func (n Name) ParamID() string {
	return fmt.Sprintf("%s_id", strings.Replace(n.UnderSingular(), "/", "_", -1))
}

// Package returns go package
func (n Name) Package() string {
	key := string(n)

	for _, gp := range envy.GoPaths() {
		key = strings.TrimPrefix(key, filepath.Join(gp, "src"))
		key = strings.TrimPrefix(key, gp)
	}
	key = strings.TrimPrefix(key, string(filepath.Separator))

	key = strings.Replace(key, "\\", "/", -1)
	return key
}

// Char returns first character in lower case, this is useful for methods inside a struct.
func (n Name) Char() string {
	return strings.ToLower(string(n[0]))
}

func (n Name) String() string {
	return string(n)
}
