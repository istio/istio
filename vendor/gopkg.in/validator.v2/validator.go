// Package validator implements value validations
//
// Copyright 2014 Roberto Teixeira <robteix@robteix.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validator

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"
)

// TextErr is an error that also implements the TextMarshaller interface for
// serializing out to various plain text encodings. Packages creating their
// own custom errors should use TextErr if they're intending to use serializing
// formats like json, msgpack etc.
type TextErr struct {
	Err error
}

// Error implements the error interface.
func (t TextErr) Error() string {
	return t.Err.Error()
}

// MarshalText implements the TextMarshaller
func (t TextErr) MarshalText() ([]byte, error) {
	return []byte(t.Err.Error()), nil
}

var (
	// ErrZeroValue is the error returned when variable has zero valud
	// and nonzero was specified
	ErrZeroValue = TextErr{errors.New("zero value")}
	// ErrMin is the error returned when variable is less than mininum
	// value specified
	ErrMin = TextErr{errors.New("less than min")}
	// ErrMax is the error returned when variable is more than
	// maximum specified
	ErrMax = TextErr{errors.New("greater than max")}
	// ErrLen is the error returned when length is not equal to
	// param specified
	ErrLen = TextErr{errors.New("invalid length")}
	// ErrRegexp is the error returned when the value does not
	// match the provided regular expression parameter
	ErrRegexp = TextErr{errors.New("regular expression mismatch")}
	// ErrUnsupported is the error error returned when a validation rule
	// is used with an unsupported variable type
	ErrUnsupported = TextErr{errors.New("unsupported type")}
	// ErrBadParameter is the error returned when an invalid parameter
	// is provided to a validation rule (e.g. a string where an int was
	// expected (max=foo,len=bar) or missing a parameter when one is required (len=))
	ErrBadParameter = TextErr{errors.New("bad parameter")}
	// ErrUnknownTag is the error returned when an unknown tag is found
	ErrUnknownTag = TextErr{errors.New("unknown tag")}
	// ErrInvalid is the error returned when variable is invalid
	// (normally a nil pointer)
	ErrInvalid = TextErr{errors.New("invalid value")}
)

// ErrorMap is a map which contains all errors from validating a struct.
type ErrorMap map[string]ErrorArray

// ErrorMap implements the Error interface so we can check error against nil.
// The returned error is if existent the first error which was added to the map.
func (err ErrorMap) Error() string {
	for k, errs := range err {
		if len(errs) > 0 {
			return fmt.Sprintf("%s: %s", k, errs.Error())
		}
	}

	return ""
}

// ErrorArray is a slice of errors returned by the Validate function.
type ErrorArray []error

// ErrorArray implements the Error interface and returns the first error as
// string if existent.
func (err ErrorArray) Error() string {
	if len(err) > 0 {
		return err[0].Error()
	}
	return ""
}

// ValidationFunc is a function that receives the value of a
// field and a parameter used for the respective validation tag.
type ValidationFunc func(v interface{}, param string) error

// Validator implements a validator
type Validator struct {
	// Tag name being used.
	tagName string
	// validationFuncs is a map of ValidationFuncs indexed
	// by their name.
	validationFuncs map[string]ValidationFunc
}

// Helper validator so users can use the
// functions directly from the package
var defaultValidator = NewValidator()

// NewValidator creates a new Validator
func NewValidator() *Validator {
	return &Validator{
		tagName: "validate",
		validationFuncs: map[string]ValidationFunc{
			"nonzero": nonzero,
			"len":     length,
			"min":     min,
			"max":     max,
			"regexp":  regex,
		},
	}
}

// SetTag allows you to change the tag name used in structs
func SetTag(tag string) {
	defaultValidator.SetTag(tag)
}

// SetTag allows you to change the tag name used in structs
func (mv *Validator) SetTag(tag string) {
	mv.tagName = tag
}

// WithTag creates a new Validator with the new tag name. It is
// useful to chain-call with Validate so we don't change the tag
// name permanently: validator.WithTag("foo").Validate(t)
func WithTag(tag string) *Validator {
	return defaultValidator.WithTag(tag)
}

// WithTag creates a new Validator with the new tag name. It is
// useful to chain-call with Validate so we don't change the tag
// name permanently: validator.WithTag("foo").Validate(t)
func (mv *Validator) WithTag(tag string) *Validator {
	v := mv.copy()
	v.SetTag(tag)
	return v
}

// Copy a validator
func (mv *Validator) copy() *Validator {
	newFuncs := map[string]ValidationFunc{}
	for k, f := range mv.validationFuncs {
		newFuncs[k] = f
	}
	return &Validator{
		tagName:         mv.tagName,
		validationFuncs: newFuncs,
	}
}

// SetValidationFunc sets the function to be used for a given
// validation constraint. Calling this function with nil vf
// is the same as removing the constraint function from the list.
func SetValidationFunc(name string, vf ValidationFunc) error {
	return defaultValidator.SetValidationFunc(name, vf)
}

// SetValidationFunc sets the function to be used for a given
// validation constraint. Calling this function with nil vf
// is the same as removing the constraint function from the list.
func (mv *Validator) SetValidationFunc(name string, vf ValidationFunc) error {
	if name == "" {
		return errors.New("name cannot be empty")
	}
	if vf == nil {
		delete(mv.validationFuncs, name)
		return nil
	}
	mv.validationFuncs[name] = vf
	return nil
}

// Validate validates the fields of a struct based
// on 'validator' tags and returns errors found indexed
// by the field name.
func Validate(v interface{}) error {
	return defaultValidator.Validate(v)
}

// Validate validates the fields of a struct based
// on 'validator' tags and returns errors found indexed
// by the field name.
func (mv *Validator) Validate(v interface{}) error {
	sv := reflect.ValueOf(v)
	st := reflect.TypeOf(v)
	if sv.Kind() == reflect.Ptr && !sv.IsNil() {
		return mv.Validate(sv.Elem().Interface())
	}
	if sv.Kind() != reflect.Struct && sv.Kind() != reflect.Interface {
		return ErrUnsupported
	}

	nfields := sv.NumField()
	m := make(ErrorMap)
	for i := 0; i < nfields; i++ {
		fname := st.Field(i).Name
		if !unicode.IsUpper(rune(fname[0])) {
			continue
		}

		f := sv.Field(i)
		// deal with pointers
		for f.Kind() == reflect.Ptr && !f.IsNil() {
			f = f.Elem()
		}
		tag := st.Field(i).Tag.Get(mv.tagName)
		if tag == "-" {
			continue
		}
		var errs ErrorArray

		if tag != "" {
			err := mv.Valid(f.Interface(), tag)
			if errors, ok := err.(ErrorArray); ok {
				errs = errors
			} else {
				if err != nil {
					errs = ErrorArray{err}
				}
			}
		}

		mv.deepValidateCollection(f, fname, m) // no-op if field is not a struct, interface, array, slice or map

		if len(errs) > 0 {
			m[st.Field(i).Name] = errs
		}
	}

	if len(m) > 0 {
		return m
	}
	return nil
}

func (mv *Validator) deepValidateCollection(f reflect.Value, fname string, m ErrorMap) {
	switch f.Kind() {
	case reflect.Struct, reflect.Interface, reflect.Ptr:
		e := mv.Validate(f.Interface())
		if e, ok := e.(ErrorMap); ok && len(e) > 0 {
			for j, k := range e {
				m[fname+"."+j] = k
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < f.Len(); i++ {
			mv.deepValidateCollection(f.Index(i), fmt.Sprintf("%s[%d]", fname, i), m)
		}
	case reflect.Map:
		for _, key := range f.MapKeys() {
			mv.deepValidateCollection(key, fmt.Sprintf("%s[%+v](key)", fname, key.Interface()), m) // validate the map key
			value := f.MapIndex(key)
			mv.deepValidateCollection(value, fmt.Sprintf("%s[%+v](value)", fname, key.Interface()), m)
		}
	}
}

// Valid validates a value based on the provided
// tags and returns errors found or nil.
func Valid(val interface{}, tags string) error {
	return defaultValidator.Valid(val, tags)
}

// Valid validates a value based on the provided
// tags and returns errors found or nil.
func (mv *Validator) Valid(val interface{}, tags string) error {
	if tags == "-" {
		return nil
	}
	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		return mv.Valid(v.Elem().Interface(), tags)
	}
	var err error
	switch v.Kind() {
	case reflect.Invalid:
		err = mv.validateVar(nil, tags)
	default:
		err = mv.validateVar(val, tags)
	}
	return err
}

// validateVar validates one single variable
func (mv *Validator) validateVar(v interface{}, tag string) error {
	tags, err := mv.parseTags(tag)
	if err != nil {
		// unknown tag found, give up.
		return err
	}
	errs := make(ErrorArray, 0, len(tags))
	for _, t := range tags {
		if err := t.Fn(v, t.Param); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// tag represents one of the tag items
type tag struct {
	Name  string         // name of the tag
	Fn    ValidationFunc // validation function to call
	Param string         // parameter to send to the validation function
}

// separate by no escaped commas
var sepPattern *regexp.Regexp = regexp.MustCompile(`((?:^|[^\\])(?:\\\\)*),`)

func splitUnescapedComma(str string) []string {
	ret := []string{}
	indexes := sepPattern.FindAllStringIndex(str, -1)
	last := 0
	for _, is := range indexes {
		ret = append(ret, str[last:is[1]-1])
		last = is[1]
	}
	ret = append(ret, str[last:])
	return ret
}

// parseTags parses all individual tags found within a struct tag.
func (mv *Validator) parseTags(t string) ([]tag, error) {
	tl := splitUnescapedComma(t)
	tags := make([]tag, 0, len(tl))
	for _, i := range tl {
		i = strings.Replace(i, `\,`, ",", -1)
		tg := tag{}
		v := strings.SplitN(i, "=", 2)
		tg.Name = strings.Trim(v[0], " ")
		if tg.Name == "" {
			return []tag{}, ErrUnknownTag
		}
		if len(v) > 1 {
			tg.Param = strings.Trim(v[1], " ")
		}
		var found bool
		if tg.Fn, found = mv.validationFuncs[tg.Name]; !found {
			return []tag{}, ErrUnknownTag
		}
		tags = append(tags, tg)

	}
	return tags, nil
}
