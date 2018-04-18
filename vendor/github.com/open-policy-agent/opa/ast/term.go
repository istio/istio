// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dchest/siphash"
	"github.com/open-policy-agent/opa/util"
	"github.com/pkg/errors"
)

// Location records a position in source code
type Location struct {
	Text []byte `json:"-"`    // The original text fragment from the source.
	File string `json:"file"` // The name of the source file (which may be empty).
	Row  int    `json:"row"`  // The line in the source.
	Col  int    `json:"col"`  // The column in the row.
}

// NewLocation returns a new Location object.
func NewLocation(text []byte, file string, row int, col int) *Location {
	return &Location{Text: text, File: file, Row: row, Col: col}
}

// Equal checks if two locations are equal to each other.
func (loc *Location) Equal(other *Location) bool {
	return bytes.Equal(loc.Text, other.Text) &&
		loc.File == other.File &&
		loc.Row == other.Row &&
		loc.Col == other.Col
}

// Errorf returns a new error value with a message formatted to include the location
// info (e.g., line, column, filename, etc.)
func (loc *Location) Errorf(f string, a ...interface{}) error {
	return errors.New(loc.Format(f, a...))
}

// Wrapf returns a new error value that wraps an existing error with a message formatted
// to include the location info (e.g., line, column, filename, etc.)
func (loc *Location) Wrapf(err error, f string, a ...interface{}) error {
	return errors.Wrap(err, loc.Format(f, a...))
}

// Format returns a formatted string prefixed with the location information.
func (loc *Location) Format(f string, a ...interface{}) string {
	if len(loc.File) > 0 {
		f = fmt.Sprintf("%v:%v: %v", loc.File, loc.Row, f)
	} else {
		f = fmt.Sprintf("%v:%v: %v", loc.Row, loc.Col, f)
	}
	return fmt.Sprintf(f, a...)
}

func (loc *Location) String() string {
	if len(loc.File) > 0 {
		return fmt.Sprintf("%v:%v", loc.File, loc.Row)
	}
	if len(loc.Text) > 0 {
		return string(loc.Text)
	}
	return fmt.Sprintf("%v:%v", loc.Row, loc.Col)
}

// Value declares the common interface for all Term values. Every kind of Term value
// in the language is represented as a type that implements this interface:
//
// - Null, Boolean, Number, String
// - Object, Array, Set
// - Variables, References
// - Array, Set, and Object Comprehensions
// - Calls
type Value interface {
	Compare(other Value) int      // Compare returns <0, 0, or >0 if this Value is less than, equal to, or greater than other, respectively.
	Find(path Ref) (Value, error) // Find returns value referred to by path or an error if path is not found.
	Hash() int                    // Returns hash code of the value.
	IsGround() bool               // IsGround returns true if this value is not a variable or contains no variables.
	String() string               // String returns a human readable string representation of the value.
}

// InterfaceToValue converts a native Go value x to a Value.
func InterfaceToValue(x interface{}) (Value, error) {
	switch x := x.(type) {
	case nil:
		return Null{}, nil
	case bool:
		return Boolean(x), nil
	case json.Number:
		return Number(x), nil
	case int64:
		return int64Number(x), nil
	case float64:
		return floatNumber(x), nil
	case int:
		return intNumber(x), nil
	case string:
		return String(x), nil
	case []interface{}:
		r := Array{}
		for _, e := range x {
			e, err := InterfaceToValue(e)
			if err != nil {
				return nil, err
			}
			r = append(r, &Term{Value: e})
		}
		return r, nil
	case map[string]interface{}:
		r := NewObject()
		for k, v := range x {
			k, err := InterfaceToValue(k)
			if err != nil {
				return nil, err
			}
			v, err := InterfaceToValue(v)
			if err != nil {
				return nil, err
			}
			r.Insert(NewTerm(k), NewTerm(v))
		}
		return r, nil
	case map[string]string:
		r := NewObject()
		for k, v := range x {
			k, err := InterfaceToValue(k)
			if err != nil {
				return nil, err
			}
			v, err := InterfaceToValue(v)
			if err != nil {
				return nil, err
			}
			r.Insert(NewTerm(k), NewTerm(v))
		}
		return r, nil
	default:
		return nil, fmt.Errorf("ast: illegal value: %T", x)
	}
}

// Resolver defines the interface for resolving references to native Go values.
type Resolver interface {
	Resolve(ref Ref) (value interface{}, err error)
}

// ValueResolver defines the interface for resolving references to AST values.
type ValueResolver interface {
	Resolve(ref Ref) (value Value, err error)
}

type illegalResolver struct{}

func (illegalResolver) Resolve(ref Ref) (interface{}, error) {
	return nil, fmt.Errorf("illegal value: %v", ref)
}

// ValueToInterface returns the Go representation of an AST value.  The AST
// value should not contain any values that require evaluation (e.g., vars,
// comprehensions, etc.)
func ValueToInterface(v Value, resolver Resolver) (interface{}, error) {
	switch v := v.(type) {
	case Null:
		return nil, nil
	case Boolean:
		return bool(v), nil
	case Number:
		return json.Number(v), nil
	case String:
		return string(v), nil
	case Array:
		buf := []interface{}{}
		for _, x := range v {
			x1, err := ValueToInterface(x.Value, resolver)
			if err != nil {
				return nil, err
			}
			buf = append(buf, x1)
		}
		return buf, nil
	case Object:
		buf := map[string]interface{}{}
		err := v.Iter(func(k, v *Term) error {
			ki, err := ValueToInterface(k.Value, resolver)
			if err != nil {
				return err
			}
			asStr, stringKey := ki.(string)
			if !stringKey {
				return fmt.Errorf("object value has non-string key (%T)", ki)
			}
			vi, err := ValueToInterface(v.Value, resolver)
			if err != nil {
				return err
			}
			buf[asStr] = vi
			return nil
		})
		if err != nil {
			return nil, err
		}
		return buf, nil
	case Set:
		buf := []interface{}{}
		err := v.Iter(func(x *Term) error {
			x1, err := ValueToInterface(x.Value, resolver)
			if err != nil {
				return err
			}
			buf = append(buf, x1)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return buf, nil
	case Ref:
		return resolver.Resolve(v)
	default:
		return nil, fmt.Errorf("%v requires evaluation", TypeName(v))
	}
}

// JSON returns the JSON representation of v. The value must not contain any
// refs or terms that require evaluation (e.g., vars, comprehensions, etc.)
func JSON(v Value) (interface{}, error) {
	return ValueToInterface(v, illegalResolver{})
}

// MustInterfaceToValue converts a native Go value x to a Value. If the
// conversion fails, this function will panic. This function is mostly for test
// purposes.
func MustInterfaceToValue(x interface{}) Value {
	v, err := InterfaceToValue(x)
	if err != nil {
		panic(err)
	}
	return v
}

// Term is an argument to a function.
type Term struct {
	Value    Value     `json:"value"` // the value of the Term as represented in Go
	Location *Location `json:"-"`     // the location of the Term in the source
}

// NewTerm returns a new Term object.
func NewTerm(v Value) *Term {
	return &Term{
		Value: v,
	}
}

// SetLocation updates the term's Location and returns the term itself.
func (term *Term) SetLocation(loc *Location) *Term {
	term.Location = loc
	return term
}

// Copy returns a deep copy of term.
func (term *Term) Copy() *Term {

	if term == nil {
		return nil
	}

	cpy := *term

	switch v := term.Value.(type) {
	case Null, Boolean, Number, String, Var:
		cpy.Value = v
	case Ref:
		cpy.Value = v.Copy()
	case Array:
		cpy.Value = v.Copy()
	case Set:
		cpy.Value = v.Copy()
	case Object:
		cpy.Value = v.Copy()
	case *ArrayComprehension:
		cpy.Value = v.Copy()
	case *ObjectComprehension:
		cpy.Value = v.Copy()
	case *SetComprehension:
		cpy.Value = v.Copy()
	case Call:
		cpy.Value = v.Copy()
	}

	return &cpy
}

// Equal returns true if this term equals the other term. Equality is
// defined for each kind of term.
func (term *Term) Equal(other *Term) bool {
	if term == nil && other != nil {
		return false
	}
	if term != nil && other == nil {
		return false
	}
	if term == other {
		return true
	}
	return term.Value.Compare(other.Value) == 0
}

// Get returns a value referred to by name from the term.
func (term *Term) Get(name *Term) *Term {
	switch v := term.Value.(type) {
	case Array:
		return v.Get(name)
	case Object:
		return v.Get(name)
	case Set:
		if v.Contains(name) {
			return name
		}
	}
	return nil
}

// Hash returns the hash code of the Term's value.
func (term *Term) Hash() int {
	return term.Value.Hash()
}

// IsGround returns true if this terms' Value is ground.
func (term *Term) IsGround() bool {
	return term.Value.IsGround()
}

// MarshalJSON returns the JSON encoding of the term.
//
// Specialized marshalling logic is required to include a type hint for Value.
func (term *Term) MarshalJSON() ([]byte, error) {
	d := map[string]interface{}{
		"type":  TypeName(term.Value),
		"value": term.Value,
	}
	return json.Marshal(d)
}

func (term *Term) String() string {
	return term.Value.String()
}

// UnmarshalJSON parses the byte array and stores the result in term.
// Specialized unmarshalling is required to handle Value.
func (term *Term) UnmarshalJSON(bs []byte) error {
	v := map[string]interface{}{}
	if err := util.UnmarshalJSON(bs, &v); err != nil {
		return err
	}
	val, err := unmarshalValue(v)
	if err != nil {
		return err
	}
	term.Value = val
	return nil
}

// Vars returns a VarSet with variables contained in this term.
func (term *Term) Vars() VarSet {
	vis := &VarVisitor{vars: VarSet{}}
	Walk(vis, term)
	return vis.vars
}

// IsConstant returns true if the AST value is constant.
func IsConstant(v Value) bool {
	found := false
	Walk(&GenericVisitor{
		func(x interface{}) bool {
			switch x.(type) {
			case Var, Ref, *ArrayComprehension, *ObjectComprehension, *SetComprehension, Call:
				found = true
				return true
			}
			return false
		},
	}, v)
	return !found
}

// IsComprehension returns true if the supplied value is a comprehension.
func IsComprehension(x Value) bool {
	switch x.(type) {
	case *ArrayComprehension, *ObjectComprehension, *SetComprehension:
		return true
	}
	return false
}

// ContainsRefs returns true if the Value v contains refs.
func ContainsRefs(v interface{}) bool {
	found := false
	WalkRefs(v, func(r Ref) bool {
		found = true
		return found
	})
	return found
}

// ContainsComprehensions returns true if the Value v contains comprehensions.
func ContainsComprehensions(v interface{}) bool {
	found := false
	WalkClosures(v, func(x interface{}) bool {
		switch x.(type) {
		case *ArrayComprehension, *ObjectComprehension, *SetComprehension:
			found = true
			return found
		}
		return found
	})
	return found
}

// IsScalar returns true if the AST value is a scalar.
func IsScalar(v Value) bool {
	switch v.(type) {
	case String:
		return true
	case Number:
		return true
	case Boolean:
		return true
	case Null:
		return true
	}
	return false
}

// Null represents the null value defined by JSON.
type Null struct{}

// NullTerm creates a new Term with a Null value.
func NullTerm() *Term {
	return &Term{Value: Null{}}
}

// Equal returns true if the other term Value is also Null.
func (null Null) Equal(other Value) bool {
	switch other.(type) {
	case Null:
		return true
	default:
		return false
	}
}

// Compare compares null to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (null Null) Compare(other Value) int {
	return Compare(null, other)
}

// Find returns the current value or a not found error.
func (null Null) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return null, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code for the Value.
func (null Null) Hash() int {
	return 0
}

// IsGround always returns true.
func (null Null) IsGround() bool {
	return true
}

func (null Null) String() string {
	return "null"
}

// Boolean represents a boolean value defined by JSON.
type Boolean bool

// BooleanTerm creates a new Term with a Boolean value.
func BooleanTerm(b bool) *Term {
	return &Term{Value: Boolean(b)}
}

// Equal returns true if the other Value is a Boolean and is equal.
func (bol Boolean) Equal(other Value) bool {
	switch other := other.(type) {
	case Boolean:
		return bol == other
	default:
		return false
	}
}

// Compare compares bol to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (bol Boolean) Compare(other Value) int {
	return Compare(bol, other)
}

// Find returns the current value or a not found error.
func (bol Boolean) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return bol, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code for the Value.
func (bol Boolean) Hash() int {
	if bol {
		return 1
	}
	return 0
}

// IsGround always returns true.
func (bol Boolean) IsGround() bool {
	return true
}

func (bol Boolean) String() string {
	return strconv.FormatBool(bool(bol))
}

// Number represents a numeric value as defined by JSON.
type Number json.Number

// NumberTerm creates a new Term with a Number value.
func NumberTerm(n json.Number) *Term {
	return &Term{Value: Number(n)}
}

// IntNumberTerm creates a new Term with an integer Number value.
func IntNumberTerm(i int) *Term {
	num := Number(json.Number(fmt.Sprintf("%d", i)))
	return &Term{Value: num}
}

// FloatNumberTerm creates a new Term with a floating point Number value.
func FloatNumberTerm(f float64) *Term {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	return &Term{Value: Number(s)}
}

// Equal returns true if the other Value is a Number and is equal.
func (num Number) Equal(other Value) bool {
	switch other := other.(type) {
	case Number:
		return Compare(num, other) == 0
	default:
		return false
	}
}

// Compare compares num to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (num Number) Compare(other Value) int {
	return Compare(num, other)
}

// Find returns the current value or a not found error.
func (num Number) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return num, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code for the Value.
func (num Number) Hash() int {
	f, err := json.Number(num).Float64()
	if err != nil {
		bs := []byte(num)
		h := siphash.Hash(hashSeed0, hashSeed1, bs)
		return int(h)
	}
	return int(f)
}

// Int returns the int representation of num if possible.
func (num Number) Int() (int, bool) {
	i, err := json.Number(num).Int64()
	if err != nil {
		return 0, false
	}
	return int(i), true
}

// IsGround always returns true.
func (num Number) IsGround() bool {
	return true
}

// MarshalJSON returns JSON encoded bytes representing num.
func (num Number) MarshalJSON() ([]byte, error) {
	return json.Marshal(json.Number(num))
}

func (num Number) String() string {
	return string(num)
}

func intNumber(i int) Number {
	return Number(json.Number(fmt.Sprintf("%d", i)))
}

func int64Number(i int64) Number {
	return Number(json.Number(fmt.Sprintf("%d", i)))
}

func floatNumber(f float64) Number {
	return Number(strconv.FormatFloat(f, 'g', -1, 64))
}

// String represents a string value as defined by JSON.
type String string

// StringTerm creates a new Term with a String value.
func StringTerm(s string) *Term {
	return &Term{Value: String(s)}
}

// Equal returns true if the other Value is a String and is equal.
func (str String) Equal(other Value) bool {
	switch other := other.(type) {
	case String:
		return str == other
	default:
		return false
	}
}

// Compare compares str to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (str String) Compare(other Value) int {
	return Compare(str, other)
}

// Find returns the current value or a not found error.
func (str String) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return str, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// IsGround always returns true.
func (str String) IsGround() bool {
	return true
}

func (str String) String() string {
	return strconv.Quote(string(str))
}

// Hash returns the hash code for the Value.
func (str String) Hash() int {
	bs := []byte(str)
	h := siphash.Hash(hashSeed0, hashSeed1, bs)
	return int(h)
}

// Var represents a variable as defined by the language.
type Var string

// VarTerm creates a new Term with a Variable value.
func VarTerm(v string) *Term {
	return &Term{Value: Var(v)}
}

// Equal returns true if the other Value is a Variable and has the same value
// (name).
func (v Var) Equal(other Value) bool {
	switch other := other.(type) {
	case Var:
		return v == other
	default:
		return false
	}
}

// Compare compares v to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (v Var) Compare(other Value) int {
	return Compare(v, other)
}

// Find returns the current value or a not found error.
func (v Var) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return v, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code for the Value.
func (v Var) Hash() int {
	bs := []byte(v)
	h := siphash.Hash(hashSeed0, hashSeed1, bs)
	return int(h)
}

// IsGround always returns false.
func (v Var) IsGround() bool {
	return false
}

// IsWildcard returns true if this is a wildcard variable.
func (v Var) IsWildcard() bool {
	return strings.HasPrefix(string(v), WildcardPrefix)
}

// IsGenerated returns true if this variable was generated during compilation.
func (v Var) IsGenerated() bool {
	return strings.HasPrefix(string(v), "__local")
}

func (v Var) String() string {
	// Special case for wildcard so that string representation is parseable. The
	// parser mangles wildcard variables to make their names unique and uses an
	// illegal variable name character (WildcardPrefix) to avoid conflicts. When
	// we serialize the variable here, we need to make sure it's parseable.
	if v.IsWildcard() {
		return Wildcard.String()
	}
	return string(v)
}

// Ref represents a reference as defined by the language.
type Ref []*Term

// EmptyRef returns a new, empty reference.
func EmptyRef() Ref {
	return Ref([]*Term{})
}

// RefTerm creates a new Term with a Ref value.
func RefTerm(r ...*Term) *Term {
	return &Term{Value: Ref(r)}
}

// Append returns a copy of ref with the term appended to the end.
func (ref Ref) Append(term *Term) Ref {
	n := len(ref)
	dst := make(Ref, n+1)
	copy(dst, ref)
	dst[n] = term
	return dst
}

// Insert returns a copy of the ref with x inserted at pos. If pos < len(ref),
// existing elements are shifted to the right. If pos > len(ref)+1 this
// function panics.
func (ref Ref) Insert(x *Term, pos int) Ref {
	if pos == len(ref) {
		return ref.Append(x)
	} else if pos > len(ref)+1 {
		panic("illegal index")
	}
	cpy := make(Ref, len(ref)+1)
	for i := 0; i < pos; i++ {
		cpy[i] = ref[i]
	}
	cpy[pos] = x
	for i := pos; i < len(ref); i++ {
		cpy[i+1] = ref[i]
	}
	return cpy
}

// Extend returns a copy of ref with the terms from other appended. The head of
// other will be converted to a string.
func (ref Ref) Extend(other Ref) Ref {
	dst := make(Ref, len(ref)+len(other))
	for i := range ref {
		dst[i] = ref[i]
	}
	head := other[0].Copy()
	head.Value = String(head.Value.(Var))
	offset := len(ref)
	dst[offset] = head
	for i := range other[1:] {
		dst[offset+i+1] = other[i+1]
	}
	return dst
}

// Dynamic returns the offset of the first non-constant operand of ref.
func (ref Ref) Dynamic() int {
	for i := 1; i < len(ref); i++ {
		if !IsConstant(ref[i].Value) {
			return i
		}
	}
	return -1
}

// Copy returns a deep copy of ref.
func (ref Ref) Copy() Ref {
	return termSliceCopy(ref)
}

// Equal returns true if ref is equal to other.
func (ref Ref) Equal(other Value) bool {
	return Compare(ref, other) == 0
}

// Compare compares ref to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (ref Ref) Compare(other Value) int {
	return Compare(ref, other)
}

// Find returns the current value or a not found error.
func (ref Ref) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return ref, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code for the Value.
func (ref Ref) Hash() int {
	return termSliceHash(ref)
}

// HasPrefix returns true if the other ref is a prefix of this ref.
func (ref Ref) HasPrefix(other Ref) bool {
	if len(other) > len(ref) {
		return false
	}
	for i := range other {
		if !ref[i].Equal(other[i]) {
			return false
		}
	}
	return true
}

// ConstantPrefix returns the constant portion of the ref starting from the head.
func (ref Ref) ConstantPrefix() Ref {
	ref = ref.Copy()

	i := ref.Dynamic()
	if i < 0 {
		return ref
	}
	return ref[:i]
}

// GroundPrefix returns the ground portion of the ref starting from the head. By
// definition, the head of the reference is always ground.
func (ref Ref) GroundPrefix() Ref {
	prefix := make(Ref, 0, len(ref))

	for i, x := range ref {
		if i > 0 && !x.IsGround() {
			break
		}
		prefix = append(prefix, x)
	}

	return prefix
}

// IsGround returns true if all of the parts of the Ref are ground.
func (ref Ref) IsGround() bool {
	if len(ref) == 0 {
		return true
	}
	return termSliceIsGround(ref[1:])
}

// IsNested returns true if this ref contains other Refs.
func (ref Ref) IsNested() bool {
	for _, x := range ref {
		if _, ok := x.Value.(Ref); ok {
			return true
		}
	}
	return false
}

var varRegexp = regexp.MustCompile("^[[:alpha:]_][[:alpha:][:digit:]_]*$")

func (ref Ref) String() string {
	if len(ref) == 0 {
		return ""
	}
	var buf []string
	path := ref
	switch v := ref[0].Value.(type) {
	case Var:
		buf = append(buf, string(v))
		path = path[1:]
	}
	for _, p := range path {
		switch p := p.Value.(type) {
		case String:
			str := string(p)
			if varRegexp.MatchString(str) && len(buf) > 0 {
				buf = append(buf, "."+str)
			} else {
				buf = append(buf, "["+p.String()+"]")
			}
		default:
			buf = append(buf, "["+p.String()+"]")
		}
	}
	return strings.Join(buf, "")
}

// OutputVars returns a VarSet containing variables that would be bound by evaluating
//  this expression in isolation.
func (ref Ref) OutputVars() VarSet {
	vis := NewVarVisitor().WithParams(VarVisitorParams{SkipRefHead: true})
	Walk(vis, ref)
	return vis.Vars()
}

// QueryIterator defines the interface for querying AST documents with references.
type QueryIterator func(map[Var]Value, Value) error

// Array represents an array as defined by the language. Arrays are similar to the
// same types as defined by JSON with the exception that they can contain Vars
// and References.
type Array []*Term

// ArrayTerm creates a new Term with an Array value.
func ArrayTerm(a ...*Term) *Term {
	return &Term{Value: Array(a)}
}

// Copy returns a deep copy of arr.
func (arr Array) Copy() Array {
	return termSliceCopy(arr)
}

// Equal returns true if arr is equal to other.
func (arr Array) Equal(other Value) bool {
	return Compare(arr, other) == 0
}

// Compare compares arr to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (arr Array) Compare(other Value) int {
	return Compare(arr, other)
}

// Find returns the value at the index or an out-of-range error.
func (arr Array) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return arr, nil
	}
	num, ok := path[0].Value.(Number)
	if !ok {
		return nil, fmt.Errorf("find: not found")
	}
	i, ok := num.Int()
	if !ok {
		return nil, fmt.Errorf("find: not found")
	}
	if i < 0 || i >= len(arr) {
		return nil, fmt.Errorf("find: not found")
	}
	return arr[i].Value.Find(path[1:])
}

// Get returns the element at pos or nil if not possible.
func (arr Array) Get(pos *Term) *Term {
	num, ok := pos.Value.(Number)
	if !ok {
		return nil
	}

	i, ok := num.Int()
	if !ok {
		return nil
	}

	if i >= 0 && i < len(arr) {
		return arr[i]
	}

	return nil
}

// Sorted returns a new Array that contains the sorted elements of arr.
func (arr Array) Sorted() Array {
	cpy := make(Array, len(arr))
	for i := range cpy {
		cpy[i] = arr[i]
	}
	sort.Sort(termSlice(cpy))
	return cpy
}

// Hash returns the hash code for the Value.
func (arr Array) Hash() int {
	return termSliceHash(arr)
}

// IsGround returns true if all of the Array elements are ground.
func (arr Array) IsGround() bool {
	return termSliceIsGround(arr)
}

// MarshalJSON returns JSON encoded bytes representing arr.
func (arr Array) MarshalJSON() ([]byte, error) {
	if len(arr) == 0 {
		return json.Marshal([]interface{}{})
	}
	return json.Marshal([]*Term(arr))
}

func (arr Array) String() string {
	var buf []string
	for _, e := range arr {
		buf = append(buf, e.String())
	}
	return "[" + strings.Join(buf, ", ") + "]"
}

// Set represents a set as defined by the language.
type Set interface {
	Value
	Len() int
	Copy() Set
	Diff(Set) Set
	Intersect(Set) Set
	Union(Set) Set
	Add(*Term)
	Iter(func(*Term) error) error
	Until(func(*Term) bool) bool
	Foreach(func(*Term))
	Contains(*Term) bool
	Map(func(*Term) (*Term, error)) (Set, error)
	Reduce(*Term, func(*Term, *Term) (*Term, error)) (*Term, error)
	Sorted() Array
}

// NewSet returns a new Set containing t.
func NewSet(t ...*Term) Set {
	s := &set{
		elems: map[int]*setElem{},
	}
	for i := range t {
		s.Add(t[i])
	}
	return s
}

// SetTerm returns a new Term representing a set containing terms t.
func SetTerm(t ...*Term) *Term {
	set := NewSet(t...)
	return &Term{
		Value: set,
	}
}

type set struct {
	elems map[int]*setElem
	keys  []*Term
}

type setElem struct {
	elem *Term
	next *setElem
}

func (s *setElem) String() string {
	buf := []string{}
	for c := s; c != nil; c = c.next {
		buf = append(buf, fmt.Sprint(c.elem))
	}
	return strings.Join(buf, "->")
}

// Copy returns a deep copy of s.
func (s *set) Copy() Set {
	cpy := NewSet()
	s.Foreach(func(x *Term) {
		cpy.Add(x)
	})
	return cpy
}

// IsGround returns true if all terms in s are ground.
func (s *set) IsGround() bool {
	return !s.Until(func(x *Term) bool {
		return !x.IsGround()
	})
}

// Hash returns a hash code for s.
func (s *set) Hash() int {
	var hash int
	s.Foreach(func(x *Term) {
		hash += x.Hash()
	})
	return hash
}

func (s *set) String() string {
	if s.Len() == 0 {
		return "set()"
	}
	buf := []string{}
	s.Foreach(func(x *Term) {
		buf = append(buf, x.String())
	})
	return "{" + strings.Join(buf, ", ") + "}"
}

// Compare compares s to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (s *set) Compare(other Value) int {
	o1 := sortOrder(s)
	o2 := sortOrder(other)
	if o1 < o2 {
		return -1
	} else if o1 > o2 {
		return 1
	}
	t := other.(*set)
	sort.Sort(termSlice(s.keys))
	sort.Sort(termSlice(t.keys))
	return termSliceCompare(s.keys, t.keys)
}

// Find returns the current value or a not found error.
func (s *set) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return s, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Diff returns elements in s that are not in other.
func (s *set) Diff(other Set) Set {
	r := NewSet()
	s.Foreach(func(x *Term) {
		if !other.Contains(x) {
			r.Add(x)
		}
	})
	return r
}

// Intersect returns the set containing elements in both s and other.
func (s *set) Intersect(other Set) Set {
	r := NewSet()
	s.Foreach(func(x *Term) {
		if other.Contains(x) {
			r.Add(x)
		}
	})
	return r
}

// Union returns the set containing all elements of s and other.
func (s *set) Union(other Set) Set {
	r := NewSet()
	s.Foreach(func(x *Term) {
		r.Add(x)
	})
	other.Foreach(func(x *Term) {
		r.Add(x)
	})
	return r
}

// Add updates s to include t.
func (s *set) Add(t *Term) {
	s.insert(t)
}

// Iter calls f on each element in s. If f returns an error, iteration stops
// and the return value is the error.
func (s *set) Iter(f func(*Term) error) error {
	for i := range s.keys {
		if err := f(s.keys[i]); err != nil {
			return err
		}
	}
	return nil
}

var errStop = errors.New("stop")

// Until calls f on each element in s. If f returns true, iteration stops.
func (s *set) Until(f func(*Term) bool) bool {
	err := s.Iter(func(t *Term) error {
		if f(t) {
			return errStop
		}
		return nil
	})
	return err != nil
}

// Foreach calls f on each element in s.
func (s *set) Foreach(f func(*Term)) {
	s.Iter(func(t *Term) error {
		f(t)
		return nil
	})
}

// Map returns a new Set obtained by applying f to each value in s.
func (s *set) Map(f func(*Term) (*Term, error)) (Set, error) {
	set := NewSet()
	err := s.Iter(func(x *Term) error {
		term, err := f(x)
		if err != nil {
			return err
		}
		set.Add(term)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}

// Reduce returns a Term produced by applying f to each value in s. The first
// argument to f is the reduced value (starting with i) and the second argument
// to f is the element in s.
func (s *set) Reduce(i *Term, f func(*Term, *Term) (*Term, error)) (*Term, error) {
	err := s.Iter(func(x *Term) error {
		var err error
		i, err = f(i, x)
		if err != nil {
			return err
		}
		return nil
	})
	return i, err
}

// Contains returns true if t is in s.
func (s *set) Contains(t *Term) bool {
	return s.get(t) != nil
}

// Len returns the number of elements in the set.
func (s *set) Len() int {
	return len(s.keys)
}

// MarshalJSON returns JSON encoded bytes representing s.
func (s *set) MarshalJSON() ([]byte, error) {
	if s.keys == nil {
		return json.Marshal([]interface{}{})
	}
	return json.Marshal(s.keys)
}

// Sorted returns an Array that contains the sorted elements of s.
func (s *set) Sorted() Array {
	cpy := make(Array, len(s.keys))
	for i := range cpy {
		cpy[i] = s.keys[i]
	}
	sort.Sort(termSlice(cpy))
	return cpy
}

func (s *set) insert(x *Term) {
	hash := x.Hash()
	head := s.elems[hash]
	for curr := head; curr != nil; curr = curr.next {
		if Compare(curr.elem, x) == 0 {
			return
		}
	}
	s.elems[hash] = &setElem{
		elem: x,
		next: head,
	}
	s.keys = append(s.keys, x)
}

func (s *set) get(x *Term) *setElem {
	hash := x.Hash()
	for curr := s.elems[hash]; curr != nil; curr = curr.next {
		if Compare(curr.elem, x) == 0 {
			return curr
		}
	}
	return nil
}

// Object represents an object as defined by the language.
type Object interface {
	Value
	Len() int
	Get(*Term) *Term
	Copy() Object
	Insert(*Term, *Term)
	Iter(func(*Term, *Term) error) error
	Until(func(*Term, *Term) bool) bool
	Foreach(func(*Term, *Term))
	Map(func(*Term, *Term) (*Term, *Term, error)) (Object, error)
	Diff(other Object) Object
	Intersect(other Object) [][3]*Term
	Merge(other Object) (Object, bool)
	Keys() []*Term
}

// NewObject creates a new Object with t.
func NewObject(t ...[2]*Term) Object {
	obj := &object{
		elems: map[int]*objectElem{},
	}
	for i := range t {
		obj.Insert(t[i][0], t[i][1])
	}
	return obj
}

// ObjectTerm creates a new Term with an Object value.
func ObjectTerm(o ...[2]*Term) *Term {
	return &Term{Value: NewObject(o...)}
}

type object struct {
	elems map[int]*objectElem
	keys  []*Term
}

type objectElem struct {
	key   *Term
	value *Term
	next  *objectElem
}

// Item is a helper for constructing an tuple containing two Terms
// representing a key/value pair in an Object.
func Item(key, value *Term) [2]*Term {
	return [2]*Term{key, value}
}

// Compare compares obj to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (obj *object) Compare(other Value) int {
	o1 := sortOrder(obj)
	o2 := sortOrder(other)
	if o1 < o2 {
		return -1
	} else if o2 < o1 {
		return 1
	}
	a := obj
	b := other.(*object)
	keysA := a.Keys()
	keysB := b.Keys()
	sort.Sort(termSlice(keysA))
	sort.Sort(termSlice(keysB))
	minLen := a.Len()
	if b.Len() < a.Len() {
		minLen = b.Len()
	}
	for i := 0; i < minLen; i++ {
		keysCmp := Compare(keysA[i], keysB[i])
		if keysCmp < 0 {
			return -1
		}
		if keysCmp > 0 {
			return 1
		}
		valA := a.Get(keysA[i])
		valB := b.Get(keysB[i])
		valCmp := Compare(valA, valB)
		if valCmp != 0 {
			return valCmp
		}
	}
	if a.Len() < b.Len() {
		return -1
	}
	if b.Len() < a.Len() {
		return 1
	}
	return 0
}

// Find returns the value at the key or undefined.
func (obj *object) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return obj, nil
	}
	value := obj.Get(path[0])
	if value == nil {
		return nil, fmt.Errorf("find: not found")
	}
	return value.Value.Find(path[1:])
}

func (obj *object) Insert(k, v *Term) {
	obj.insert(k, v)
}

// Get returns the value of k in obj if k exists, otherwise nil.
func (obj *object) Get(k *Term) *Term {
	if elem := obj.get(k); elem != nil {
		return elem.value
	}
	return nil
}

// Hash returns the hash code for the Value.
func (obj *object) Hash() int {
	var hash int
	obj.Foreach(func(k, v *Term) {
		hash += v.Value.Hash()
		hash += v.Value.Hash()
	})
	return hash
}

// IsGround returns true if all of the Object key/value pairs are ground.
func (obj *object) IsGround() bool {
	return !obj.Until(func(k, v *Term) bool {
		return !k.IsGround() || !v.IsGround()
	})
}

// Copy returns a deep copy of obj.
func (obj *object) Copy() Object {
	cpy, _ := obj.Map(func(k, v *Term) (*Term, *Term, error) {
		return k.Copy(), v.Copy(), nil
	})
	return cpy
}

// Diff returns a new Object that contains only the key/value pairs that exist in obj.
func (obj *object) Diff(other Object) Object {
	r := NewObject()
	obj.Foreach(func(k, v *Term) {
		if other.Get(k) == nil {
			r.Insert(k, v)
		}
	})
	return r
}

// Intersect returns a slice of term triplets that represent the intersection of keys
// between obj and other. For each intersecting key, the values from obj and other are included
// as the last two terms in the triplet (respectively).
func (obj *object) Intersect(other Object) [][3]*Term {
	r := [][3]*Term{}
	obj.Foreach(func(k, v *Term) {
		if v2 := other.Get(k); v2 != nil {
			r = append(r, [3]*Term{k, v, v2})
		}
	})
	return r
}

// Iter calls the function f for each key-value pair in the object. If f
// returns an error, iteration stops and the error is returned.
func (obj *object) Iter(f func(*Term, *Term) error) error {
	for i := range obj.keys {
		k := obj.keys[i]
		node := obj.get(k)
		if node == nil {
			panic("corrupt object")
		}
		if err := f(k, node.value); err != nil {
			return err
		}
	}
	return nil
}

// Until calls f for each key-value pair in the object. If f returns true,
// iteration stops.
func (obj *object) Until(f func(*Term, *Term) bool) bool {
	err := obj.Iter(func(k, v *Term) error {
		if f(k, v) {
			return errStop
		}
		return nil
	})
	return err != nil
}

// Foreach calls f for each key-value pair in the object.
func (obj *object) Foreach(f func(*Term, *Term)) {
	obj.Iter(func(k, v *Term) error {
		f(k, v)
		return nil
	})
}

// Map returns a new Object constructed by mapping each element in the object
// using the function f.
func (obj *object) Map(f func(*Term, *Term) (*Term, *Term, error)) (Object, error) {
	cpy := &object{
		elems: make(map[int]*objectElem, obj.Len()),
	}
	err := obj.Iter(func(k, v *Term) error {
		var err error
		k, v, err = f(k, v)
		if err != nil {
			return err
		}
		cpy.insert(k, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cpy, nil
}

// Keys returns the keys of obj.
func (obj *object) Keys() []*Term {
	return obj.keys
}

// MarshalJSON returns JSON encoded bytes representing obj.
func (obj *object) MarshalJSON() ([]byte, error) {
	sl := make([][2]*Term, obj.Len())
	for i := range obj.keys {
		k := obj.keys[i]
		sl[i] = Item(k, obj.get(k).value)
	}
	return json.Marshal(sl)
}

// Merge returns a new Object containing the non-overlapping keys of obj and other. If there are
// overlapping keys between obj and other, the values of associated with the keys are merged. Only
// objects can be merged with other objects. If the values cannot be merged, the second turn value
// will be false.
func (obj object) Merge(other Object) (result Object, ok bool) {
	result = NewObject()
	stop := obj.Until(func(k, v *Term) bool {
		if v2 := other.Get(k); v2 == nil {
			result.Insert(k, v)
		} else {
			obj1, ok1 := v.Value.(Object)
			obj2, ok2 := v2.Value.(Object)
			if !ok1 || !ok2 {
				return true
			}
			obj3, ok := obj1.Merge(obj2)
			if !ok {
				return true
			}
			result.Insert(k, NewTerm(obj3))
		}
		return false
	})
	if stop {
		return nil, false
	}
	other.Foreach(func(k, v *Term) {
		if v2 := obj.Get(k); v2 == nil {
			result.Insert(k, v)
		}
	})
	return result, true
}

// Len returns the number of elements in the object.
func (obj object) Len() int {
	return len(obj.keys)
}

func (obj object) String() string {
	var buf []string
	obj.Foreach(func(k, v *Term) {
		buf = append(buf, fmt.Sprintf("%s: %s", k, v))
	})
	return "{" + strings.Join(buf, ", ") + "}"
}

func (obj *object) get(k *Term) *objectElem {
	hash := k.Hash()
	for curr := obj.elems[hash]; curr != nil; curr = curr.next {
		if Compare(curr.key, k) == 0 {
			return curr
		}
	}
	return nil
}

func (obj *object) insert(k, v *Term) {
	hash := k.Hash()
	head := obj.elems[hash]
	for curr := head; curr != nil; curr = curr.next {
		if Compare(curr.key, k) == 0 {
			curr.value = v
			return
		}
	}
	obj.elems[hash] = &objectElem{
		key:   k,
		value: v,
		next:  head,
	}
	obj.keys = append(obj.keys, k)
}

// ArrayComprehension represents an array comprehension as defined in the language.
type ArrayComprehension struct {
	Term *Term `json:"term"`
	Body Body  `json:"body"`
}

// ArrayComprehensionTerm creates a new Term with an ArrayComprehension value.
func ArrayComprehensionTerm(term *Term, body Body) *Term {
	return &Term{
		Value: &ArrayComprehension{
			Term: term,
			Body: body,
		},
	}
}

// Copy returns a deep copy of ac.
func (ac *ArrayComprehension) Copy() *ArrayComprehension {
	cpy := *ac
	cpy.Body = ac.Body.Copy()
	cpy.Term = ac.Term.Copy()
	return &cpy
}

// Equal returns true if ac is equal to other.
func (ac *ArrayComprehension) Equal(other Value) bool {
	return Compare(ac, other) == 0
}

// Compare compares ac to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (ac *ArrayComprehension) Compare(other Value) int {
	return Compare(ac, other)
}

// Find returns the current value or a not found error.
func (ac *ArrayComprehension) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return ac, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code of the Value.
func (ac *ArrayComprehension) Hash() int {
	return ac.Term.Hash() + ac.Body.Hash()
}

// IsGround returns true if the Term and Body are ground.
func (ac *ArrayComprehension) IsGround() bool {
	return ac.Term.IsGround() && ac.Body.IsGround()
}

func (ac *ArrayComprehension) String() string {
	return "[" + ac.Term.String() + " | " + ac.Body.String() + "]"
}

// ObjectComprehension represents an object comprehension as defined in the language.
type ObjectComprehension struct {
	Key   *Term `json:"key"`
	Value *Term `json:"value"`
	Body  Body  `json:"body"`
}

// ObjectComprehensionTerm creates a new Term with an ObjectComprehension value.
func ObjectComprehensionTerm(key, value *Term, body Body) *Term {
	return &Term{
		Value: &ObjectComprehension{
			Key:   key,
			Value: value,
			Body:  body,
		},
	}
}

// Copy returns a deep copy of oc.
func (oc *ObjectComprehension) Copy() *ObjectComprehension {
	cpy := *oc
	cpy.Body = oc.Body.Copy()
	cpy.Key = oc.Key.Copy()
	cpy.Value = oc.Value.Copy()
	return &cpy
}

// Equal returns true if oc is equal to other.
func (oc *ObjectComprehension) Equal(other Value) bool {
	return Compare(oc, other) == 0
}

// Compare compares oc to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (oc *ObjectComprehension) Compare(other Value) int {
	return Compare(oc, other)
}

// Find returns the current value or a not found error.
func (oc *ObjectComprehension) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return oc, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code of the Value.
func (oc *ObjectComprehension) Hash() int {
	return oc.Key.Hash() + oc.Value.Hash() + oc.Body.Hash()
}

// IsGround returns true if the Key, Value and Body are ground.
func (oc *ObjectComprehension) IsGround() bool {
	return oc.Key.IsGround() && oc.Value.IsGround() && oc.Body.IsGround()
}

func (oc *ObjectComprehension) String() string {
	return "{" + oc.Key.String() + ": " + oc.Value.String() + " | " + oc.Body.String() + "}"
}

// SetComprehension represents a set comprehension as defined in the language.
type SetComprehension struct {
	Term *Term `json:"term"`
	Body Body  `json:"body"`
}

// SetComprehensionTerm creates a new Term with an SetComprehension value.
func SetComprehensionTerm(term *Term, body Body) *Term {
	return &Term{
		Value: &SetComprehension{
			Term: term,
			Body: body,
		},
	}
}

// Copy returns a deep copy of sc.
func (sc *SetComprehension) Copy() *SetComprehension {
	cpy := *sc
	cpy.Body = sc.Body.Copy()
	cpy.Term = sc.Term.Copy()
	return &cpy
}

// Equal returns true if sc is equal to other.
func (sc *SetComprehension) Equal(other Value) bool {
	return Compare(sc, other) == 0
}

// Compare compares sc to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (sc *SetComprehension) Compare(other Value) int {
	return Compare(sc, other)
}

// Find returns the current value or a not found error.
func (sc *SetComprehension) Find(path Ref) (Value, error) {
	if len(path) == 0 {
		return sc, nil
	}
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code of the Value.
func (sc *SetComprehension) Hash() int {
	return sc.Term.Hash() + sc.Body.Hash()
}

// IsGround returns true if the Term and Body are ground.
func (sc *SetComprehension) IsGround() bool {
	return sc.Term.IsGround() && sc.Body.IsGround()
}

func (sc *SetComprehension) String() string {
	return "{" + sc.Term.String() + " | " + sc.Body.String() + "}"
}

// Call represents as function call in the language.
type Call []*Term

// CallTerm returns a new Term with a Call value defined by terms. The first
// term is the operator and the rest are operands.
func CallTerm(terms ...*Term) *Term {
	return NewTerm(Call(terms))
}

// Copy returns a deep copy of c.
func (c Call) Copy() Call {
	return termSliceCopy(c)
}

// Compare compares c to other, return <0, 0, or >0 if it is less than, equal to,
// or greater than other.
func (c Call) Compare(other Value) int {
	return Compare(c, other)
}

// Find returns the current value or a not found error.
func (c Call) Find(Ref) (Value, error) {
	return nil, fmt.Errorf("find: not found")
}

// Hash returns the hash code for the Value.
func (c Call) Hash() int {
	return termSliceHash(c)
}

// IsGround returns true if the Value is ground.
func (c Call) IsGround() bool {
	return termSliceIsGround(c)
}

// MakeExpr returns an ew Expr from this call.
func (c Call) MakeExpr(output *Term) *Expr {
	terms := []*Term(c)
	return NewExpr(append(terms, output))
}

func (c Call) String() string {
	args := make([]string, len(c)-1)
	for i := 1; i < len(c); i++ {
		args[i-1] = c[i].String()
	}
	return fmt.Sprintf("%v(%v)", c[0], strings.Join(args, ", "))
}

func formatString(s String) string {
	str := string(s)
	if varRegexp.MatchString(str) {
		return str
	}
	return s.String()
}

func termSliceCopy(a []*Term) []*Term {
	cpy := make([]*Term, len(a))
	for i := range a {
		cpy[i] = a[i].Copy()
	}
	return cpy
}

func termSliceEqual(a, b []*Term) bool {
	if len(a) == len(b) {
		for i := range a {
			if !a[i].Equal(b[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func termSliceHash(a []*Term) int {
	var hash int
	for _, v := range a {
		hash += v.Value.Hash()
	}
	return hash
}

func termSliceIsGround(a []*Term) bool {
	for _, v := range a {
		if !v.IsGround() {
			return false
		}
	}
	return true
}

// NOTE(tsandall): The unmarshalling errors in these functions are not
// helpful for callers because they do not identify the source of the
// unmarshalling error. Because OPA doesn't accept JSON describing ASTs
// from callers, this is acceptable (for now). If that changes in the future,
// the error messages should be revisited. The current approach focuses
// on the happy path and treats all errors the same. If better error
// reporting is needed, the error paths will need to be fleshed out.

func unmarshalBody(b []interface{}) (Body, error) {
	buf := Body{}
	for _, e := range b {
		if m, ok := e.(map[string]interface{}); ok {
			expr := &Expr{}
			if err := unmarshalExpr(expr, m); err == nil {
				buf = append(buf, expr)
				continue
			}
		}
		goto unmarshal_error
	}
	return buf, nil
unmarshal_error:
	return nil, fmt.Errorf("ast: unable to unmarshal body")
}

func unmarshalExpr(expr *Expr, v map[string]interface{}) error {
	if x, ok := v["negated"]; ok {
		if b, ok := x.(bool); ok {
			expr.Negated = b
		} else {
			return fmt.Errorf("ast: unable to unmarshal negated field with type: %T (expected true or false)", v["negated"])
		}
	}
	if err := unmarshalExprIndex(expr, v); err != nil {
		return err
	}
	switch ts := v["terms"].(type) {
	case map[string]interface{}:
		t, err := unmarshalTerm(ts)
		if err != nil {
			return err
		}
		expr.Terms = t
	case []interface{}:
		terms, err := unmarshalTermSlice(ts)
		if err != nil {
			return err
		}
		expr.Terms = terms
	default:
		return fmt.Errorf(`ast: unable to unmarshal terms field with type: %T (expected {"value": ..., "type": ...} or [{"value": ..., "type": ...}, ...])`, v["terms"])
	}
	if x, ok := v["with"]; ok {
		if sl, ok := x.([]interface{}); ok {
			ws := make([]*With, len(sl))
			for i := range sl {
				var err error
				ws[i], err = unmarshalWith(sl[i])
				if err != nil {
					return err
				}
			}
			expr.With = ws
		}
	}
	return nil
}

func unmarshalExprIndex(expr *Expr, v map[string]interface{}) error {
	if x, ok := v["index"]; ok {
		if n, ok := x.(json.Number); ok {
			i, err := n.Int64()
			if err == nil {
				expr.Index = int(i)
				return nil
			}
		}
	}
	return fmt.Errorf("ast: unable to unmarshal index field with type: %T (expected integer)", v["index"])
}

func unmarshalTerm(m map[string]interface{}) (*Term, error) {
	v, err := unmarshalValue(m)
	if err != nil {
		return nil, err
	}
	return &Term{Value: v}, nil
}

func unmarshalTermSlice(s []interface{}) ([]*Term, error) {
	buf := []*Term{}
	for _, x := range s {
		if m, ok := x.(map[string]interface{}); ok {
			if t, err := unmarshalTerm(m); err == nil {
				buf = append(buf, t)
				continue
			} else {
				return nil, err
			}
		}
		return nil, fmt.Errorf("ast: unable to unmarshal term")
	}
	return buf, nil
}

func unmarshalTermSliceValue(d map[string]interface{}) ([]*Term, error) {
	if s, ok := d["value"].([]interface{}); ok {
		return unmarshalTermSlice(s)
	}
	return nil, fmt.Errorf(`ast: unable to unmarshal term (expected {"value": [...], "type": ...} where type is one of: ref, array, or set)`)
}

func unmarshalWith(i interface{}) (*With, error) {
	if m, ok := i.(map[string]interface{}); ok {
		tgt, _ := m["target"].(map[string]interface{})
		target, err := unmarshalTerm(tgt)
		if err == nil {
			val, _ := m["value"].(map[string]interface{})
			value, err := unmarshalTerm(val)
			if err == nil {
				return &With{
					Target: target,
					Value:  value,
				}, nil
			}
			return nil, err
		}
		return nil, err
	}
	return nil, fmt.Errorf(`ast: unable to unmarshal with modifier (expected {"target": {...}, "value": {...}})`)
}

func unmarshalValue(d map[string]interface{}) (Value, error) {
	v := d["value"]
	switch d["type"] {
	case "null":
		return Null{}, nil
	case "boolean":
		if b, ok := v.(bool); ok {
			return Boolean(b), nil
		}
	case "number":
		if n, ok := v.(json.Number); ok {
			return Number(n), nil
		}
	case "string":
		if s, ok := v.(string); ok {
			return String(s), nil
		}
	case "var":
		if s, ok := v.(string); ok {
			return Var(s), nil
		}
	case "ref":
		if s, err := unmarshalTermSliceValue(d); err == nil {
			return Ref(s), nil
		}
	case "array":
		if s, err := unmarshalTermSliceValue(d); err == nil {
			return Array(s), nil
		}
	case "set":
		if s, err := unmarshalTermSliceValue(d); err == nil {
			set := NewSet()
			for _, x := range s {
				set.Add(x)
			}
			return set, nil
		}
	case "object":
		if s, ok := v.([]interface{}); ok {
			buf := NewObject()
			for _, x := range s {
				if i, ok := x.([]interface{}); ok && len(i) == 2 {
					p, err := unmarshalTermSlice(i)
					if err == nil {
						buf.Insert(p[0], p[1])
						continue
					}
				}
				goto unmarshal_error
			}
			return buf, nil
		}
	case "arraycomprehension", "setcomprehension":
		if m, ok := v.(map[string]interface{}); ok {
			t, ok := m["term"].(map[string]interface{})
			if !ok {
				goto unmarshal_error
			}

			term, err := unmarshalTerm(t)
			if err != nil {
				goto unmarshal_error
			}

			b, ok := m["body"].([]interface{})
			if !ok {
				goto unmarshal_error
			}

			body, err := unmarshalBody(b)
			if err != nil {
				goto unmarshal_error
			}

			if d["type"] == "arraycomprehension" {
				return &ArrayComprehension{Term: term, Body: body}, nil
			}
			return &SetComprehension{Term: term, Body: body}, nil
		}
	case "objectcomprehension":
		if m, ok := v.(map[string]interface{}); ok {
			k, ok := m["key"].(map[string]interface{})
			if !ok {
				goto unmarshal_error
			}

			key, err := unmarshalTerm(k)
			if err != nil {
				goto unmarshal_error
			}

			v, ok := m["value"].(map[string]interface{})
			if !ok {
				goto unmarshal_error
			}

			value, err := unmarshalTerm(v)
			if err != nil {
				goto unmarshal_error
			}

			b, ok := m["body"].([]interface{})
			if !ok {
				goto unmarshal_error
			}

			body, err := unmarshalBody(b)
			if err != nil {
				goto unmarshal_error
			}

			return &ObjectComprehension{Key: key, Value: value, Body: body}, nil
		}
	case "call":
		if s, err := unmarshalTermSliceValue(d); err == nil {
			return Call(s), nil
		}
	}
unmarshal_error:
	return nil, fmt.Errorf("ast: unable to unmarshal term")
}
