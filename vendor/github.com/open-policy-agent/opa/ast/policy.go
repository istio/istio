// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/util"
)

// Initialize seed for term hashing. This is intentionally placed before the
// root document sets are constructed to ensure they use the same hash seed as
// subsequent lookups. If the hash seeds are out of sync, lookups will fail.
var hashSeed = rand.New(rand.NewSource(time.Now().UnixNano()))
var hashSeed0 = (uint64(hashSeed.Uint32()) << 32) | uint64(hashSeed.Uint32())
var hashSeed1 = (uint64(hashSeed.Uint32()) << 32) | uint64(hashSeed.Uint32())

// DefaultRootDocument is the default root document.
//
// All package directives inside source files are implicitly prefixed with the
// DefaultRootDocument value.
var DefaultRootDocument = VarTerm("data")

// InputRootDocument names the document containing query arguments.
var InputRootDocument = VarTerm("input")

// RootDocumentNames contains the names of top-level documents that can be
// referred to in modules and queries.
var RootDocumentNames = NewSet(
	DefaultRootDocument,
	InputRootDocument,
)

// DefaultRootRef is a reference to the root of the default document.
//
// All refs to data in the policy engine's storage layer are prefixed with this ref.
var DefaultRootRef = Ref{DefaultRootDocument}

// InputRootRef is a reference to the root of the input document.
//
// All refs to query arguments are prefixed with this ref.
var InputRootRef = Ref{InputRootDocument}

// RootDocumentRefs contains the prefixes of top-level documents that all
// non-local references start with.
var RootDocumentRefs = NewSet(
	NewTerm(DefaultRootRef),
	NewTerm(InputRootRef),
)

// SystemDocumentKey is the name of the top-level key that identifies the system
// document.
var SystemDocumentKey = String("system")

// ReservedVars is the set of names that refer to implicitly ground vars.
var ReservedVars = NewVarSet(
	DefaultRootDocument.Value.(Var),
	InputRootDocument.Value.(Var),
)

// Wildcard represents the wildcard variable as defined in the language.
var Wildcard = &Term{Value: Var("_")}

// WildcardPrefix is the special character that all wildcard variables are
// prefixed with when the statement they are contained in is parsed.
var WildcardPrefix = "$"

// Keywords contains strings that map to language keywords.
var Keywords = [...]string{
	"not",
	"package",
	"import",
	"as",
	"default",
	"else",
	"with",
	"null",
	"true",
	"false",
}

// IsKeyword returns true if s is a language keyword.
func IsKeyword(s string) bool {
	for _, x := range Keywords {
		if x == s {
			return true
		}
	}
	return false
}

type (
	// Statement represents a single statement in a policy module.
	Statement interface {
		Loc() *Location
	}
)

type (

	// Module represents a collection of policies (defined by rules)
	// within a namespace (defined by the package) and optional
	// dependencies on external documents (defined by imports).
	Module struct {
		Package  *Package   `json:"package"`
		Imports  []*Import  `json:"imports,omitempty"`
		Rules    []*Rule    `json:"rules,omitempty"`
		Comments []*Comment `json:"comments,omitempty"`
	}

	// Comment contains the raw text from the comment in the definition.
	Comment struct {
		Text     []byte
		Location *Location
	}

	// Package represents the namespace of the documents produced
	// by rules inside the module.
	Package struct {
		Location *Location `json:"-"`
		Path     Ref       `json:"path"`
	}

	// Import represents a dependency on a document outside of the policy
	// namespace. Imports are optional.
	Import struct {
		Location *Location `json:"-"`
		Path     *Term     `json:"path"`
		Alias    Var       `json:"alias,omitempty"`
	}

	// Rule represents a rule as defined in the language. Rules define the
	// content of documents that represent policy decisions.
	Rule struct {
		Location *Location `json:"-"`
		Default  bool      `json:"default,omitempty"`
		Head     *Head     `json:"head"`
		Body     Body      `json:"body"`
		Else     *Rule     `json:"else,omitempty"`

		// Module is a pointer to the module containing this rule. If the rule
		// was NOT created while parsing/constructing a module, this should be
		// left unset. The pointer is not included in any standard operations
		// on the rule (e.g., printing, comparison, visiting, etc.)
		Module *Module `json:"-"`
	}

	// Head represents the head of a rule.
	Head struct {
		Location *Location `json:"-"`
		Name     Var       `json:"name"`
		Args     Args      `json:"args,omitempty"`
		Key      *Term     `json:"key,omitempty"`
		Value    *Term     `json:"value,omitempty"`
	}

	// Args represents zero or more arguments to a rule.
	Args []*Term

	// Body represents one or more expressions contained inside a rule or user
	// function.
	Body []*Expr

	// Expr represents a single expression contained inside the body of a rule.
	Expr struct {
		Location  *Location   `json:"-"`
		Generated bool        `json:"generated,omitempty"`
		Index     int         `json:"index"`
		Negated   bool        `json:"negated,omitempty"`
		Terms     interface{} `json:"terms"`
		With      []*With     `json:"with,omitempty"`
	}

	// With represents a modifier on an expression.
	With struct {
		Location *Location `json:"-"`
		Target   *Term     `json:"target"`
		Value    *Term     `json:"value"`
	}
)

// Compare returns an integer indicating whether mod is less than, equal to,
// or greater than other.
func (mod *Module) Compare(other *Module) int {
	if mod == nil {
		if other == nil {
			return 0
		}
		return -1
	} else if other == nil {
		return 1
	}
	if cmp := mod.Package.Compare(other.Package); cmp != 0 {
		return cmp
	}
	if cmp := importsCompare(mod.Imports, other.Imports); cmp != 0 {
		return cmp
	}
	return rulesCompare(mod.Rules, other.Rules)
}

// Copy returns a deep copy of mod.
func (mod *Module) Copy() *Module {
	cpy := *mod
	cpy.Rules = make([]*Rule, len(mod.Rules))
	for i := range mod.Rules {
		cpy.Rules[i] = mod.Rules[i].Copy()
	}
	cpy.Imports = make([]*Import, len(mod.Imports))
	for i := range mod.Imports {
		cpy.Imports[i] = mod.Imports[i].Copy()
	}
	cpy.Package = mod.Package.Copy()
	return &cpy
}

// Equal returns true if mod equals other.
func (mod *Module) Equal(other *Module) bool {
	return mod.Compare(other) == 0
}

func (mod *Module) String() string {
	buf := []string{}
	buf = append(buf, mod.Package.String())
	if len(mod.Imports) > 0 {
		buf = append(buf, "")
		for _, imp := range mod.Imports {
			buf = append(buf, imp.String())
		}
	}
	if len(mod.Rules) > 0 {
		buf = append(buf, "")
		for _, rule := range mod.Rules {
			buf = append(buf, rule.String())
		}
	}
	return strings.Join(buf, "\n")
}

// RuleSet returns a RuleSet containing named rules in the mod.
func (mod *Module) RuleSet(name Var) RuleSet {
	rs := NewRuleSet()
	for _, rule := range mod.Rules {
		if rule.Head.Name.Equal(name) {
			rs.Add(rule)
		}
	}
	return rs
}

// NewComment returns a new Comment object.
func NewComment(text []byte) *Comment {
	return &Comment{
		Text: text,
	}
}

// Loc returns the location of the comment in the definition.
func (c *Comment) Loc() *Location {
	return c.Location
}

func (c *Comment) String() string {
	return "#" + string(c.Text)
}

// Compare returns an integer indicating whether pkg is less than, equal to,
// or greater than other.
func (pkg *Package) Compare(other *Package) int {
	return Compare(pkg.Path, other.Path)
}

// Copy returns a deep copy of pkg.
func (pkg *Package) Copy() *Package {
	cpy := *pkg
	cpy.Path = pkg.Path.Copy()
	return &cpy
}

// Equal returns true if pkg is equal to other.
func (pkg *Package) Equal(other *Package) bool {
	return pkg.Compare(other) == 0
}

// Loc returns the location of the Package in the definition.
func (pkg *Package) Loc() *Location {
	return pkg.Location
}

func (pkg *Package) String() string {
	// Omit head as all packages have the DefaultRootDocument prepended at parse time.
	path := make(Ref, len(pkg.Path)-1)
	path[0] = VarTerm(string(pkg.Path[1].Value.(String)))
	copy(path[1:], pkg.Path[2:])
	return fmt.Sprintf("package %v", path)
}

// IsValidImportPath returns an error indicating if the import path is invalid.
// If the import path is invalid, err is nil.
func IsValidImportPath(v Value) (err error) {
	switch v := v.(type) {
	case Var:
		if !v.Equal(DefaultRootDocument.Value) && !v.Equal(InputRootDocument.Value) {
			return fmt.Errorf("invalid path %v: path must begin with input or data", v)
		}
	case Ref:
		if err := IsValidImportPath(v[0].Value); err != nil {
			return fmt.Errorf("invalid path %v: path must begin with input or data", v)
		}
		for _, e := range v[1:] {
			if _, ok := e.Value.(String); !ok {
				return fmt.Errorf("invalid path %v: path elements must be strings", v)
			}
		}
	default:
		return fmt.Errorf("invalid path %v: path must be ref or var", v)
	}
	return nil
}

// Compare returns an integer indicating whether imp is less than, equal to,
// or greater than other.
func (imp *Import) Compare(other *Import) int {
	if imp == nil {
		if other == nil {
			return 0
		}
		return -1
	} else if other == nil {
		return 1
	}
	if cmp := Compare(imp.Path, other.Path); cmp != 0 {
		return cmp
	}
	return Compare(imp.Alias, other.Alias)
}

// Copy returns a deep copy of imp.
func (imp *Import) Copy() *Import {
	cpy := *imp
	cpy.Path = imp.Path.Copy()
	return &cpy
}

// Equal returns true if imp is equal to other.
func (imp *Import) Equal(other *Import) bool {
	return imp.Compare(other) == 0
}

// Loc returns the location of the Import in the definition.
func (imp *Import) Loc() *Location {
	return imp.Location
}

// Name returns the variable that is used to refer to the imported virtual
// document. This is the alias if defined otherwise the last element in the
// path.
func (imp *Import) Name() Var {
	if len(imp.Alias) != 0 {
		return imp.Alias
	}
	switch v := imp.Path.Value.(type) {
	case Var:
		return v
	case Ref:
		if len(v) == 1 {
			return v[0].Value.(Var)
		}
		return Var(v[len(v)-1].Value.(String))
	}
	panic("illegal import")
}

func (imp *Import) String() string {
	buf := []string{"import", imp.Path.String()}
	if len(imp.Alias) > 0 {
		buf = append(buf, "as "+imp.Alias.String())
	}
	return strings.Join(buf, " ")
}

// Compare returns an integer indicating whether rule is less than, equal to,
// or greater than other.
func (rule *Rule) Compare(other *Rule) int {
	if rule == nil {
		if other == nil {
			return 0
		}
		return -1
	} else if other == nil {
		return 1
	}
	if cmp := rule.Head.Compare(other.Head); cmp != 0 {
		return cmp
	}
	if cmp := util.Compare(rule.Default, other.Default); cmp != 0 {
		return cmp
	}
	if cmp := rule.Body.Compare(other.Body); cmp != 0 {
		return cmp
	}
	return rule.Else.Compare(other.Else)
}

// Copy returns a deep copy of rule.
func (rule *Rule) Copy() *Rule {
	cpy := *rule
	cpy.Head = rule.Head.Copy()
	cpy.Body = rule.Body.Copy()
	if cpy.Else != nil {
		cpy.Else = rule.Else.Copy()
	}
	return &cpy
}

// Equal returns true if rule is equal to other.
func (rule *Rule) Equal(other *Rule) bool {
	return rule.Compare(other) == 0
}

// Loc returns the location of the Rule in the definition.
func (rule *Rule) Loc() *Location {
	return rule.Location
}

// Path returns a ref referring to the document produced by this rule. If rule
// is not contained in a module, this function panics.
func (rule *Rule) Path() Ref {
	if rule.Module == nil {
		panic("assertion failed")
	}
	return rule.Module.Package.Path.Append(StringTerm(string(rule.Head.Name)))
}

func (rule *Rule) String() string {
	buf := []string{}
	if rule.Default {
		buf = append(buf, "default")
	}
	buf = append(buf, rule.Head.String())
	if !rule.Default {
		buf = append(buf, "{")
		buf = append(buf, rule.Body.String())
		buf = append(buf, "}")
	}
	if rule.Else != nil {
		buf = append(buf, rule.Else.elseString())
	}
	return strings.Join(buf, " ")
}

func (rule *Rule) elseString() string {
	var buf []string

	buf = append(buf, "else")

	value := rule.Head.Value
	if value != nil {
		buf = append(buf, "=")
		buf = append(buf, value.String())
	}

	buf = append(buf, "{")
	buf = append(buf, rule.Body.String())
	buf = append(buf, "}")

	if rule.Else != nil {
		buf = append(buf, rule.Else.elseString())
	}

	return strings.Join(buf, " ")
}

// NewHead returns a new Head object. If args are provided, the first will be
// used for the key and the second will be used for the value.
func NewHead(name Var, args ...*Term) *Head {
	head := &Head{
		Name: name,
	}
	if len(args) == 0 {
		return head
	}
	head.Key = args[0]
	if len(args) == 1 {
		return head
	}
	head.Value = args[1]
	return head
}

// DocKind represents the collection of document types that can be produced by rules.
type DocKind int

const (
	// CompleteDoc represents a document that is completely defined by the rule.
	CompleteDoc = iota

	// PartialSetDoc represents a set document that is partially defined by the rule.
	PartialSetDoc = iota

	// PartialObjectDoc represents an object document that is partially defined by the rule.
	PartialObjectDoc = iota
)

// DocKind returns the type of document produced by this rule.
func (head *Head) DocKind() DocKind {
	if head.Key != nil {
		if head.Value != nil {
			return PartialObjectDoc
		}
		return PartialSetDoc
	}
	return CompleteDoc
}

// Compare returns an integer indicating whether head is less than, equal to,
// or greater than other.
func (head *Head) Compare(other *Head) int {
	if head == nil {
		if other == nil {
			return 0
		}
		return -1
	} else if other == nil {
		return 1
	}
	if cmp := Compare(head.Args, other.Args); cmp != 0 {
		return cmp
	}
	if cmp := Compare(head.Name, other.Name); cmp != 0 {
		return cmp
	}
	if cmp := Compare(head.Key, other.Key); cmp != 0 {
		return cmp
	}
	return Compare(head.Value, other.Value)
}

// Copy returns a deep copy of head.
func (head *Head) Copy() *Head {
	cpy := *head
	cpy.Args = head.Args.Copy()
	cpy.Key = head.Key.Copy()
	cpy.Value = head.Value.Copy()
	return &cpy
}

// Equal returns true if this head equals other.
func (head *Head) Equal(other *Head) bool {
	return head.Compare(other) == 0
}

func (head *Head) String() string {
	var buf []string
	if len(head.Args) != 0 {
		buf = append(buf, head.Name.String()+head.Args.String())
	} else if head.Key != nil {
		buf = append(buf, head.Name.String()+"["+head.Key.String()+"]")
	} else {
		buf = append(buf, head.Name.String())
	}
	if head.Value != nil {
		buf = append(buf, "=")
		buf = append(buf, head.Value.String())
	}
	return strings.Join(buf, " ")
}

// Vars returns a set of vars found in the head.
func (head *Head) Vars() VarSet {
	vis := &VarVisitor{vars: VarSet{}}
	// FIXME(tsandall): include args?
	if head.Key != nil {
		Walk(vis, head.Key)
	}
	if head.Value != nil {
		Walk(vis, head.Value)
	}
	return vis.vars
}

// Copy returns a deep copy of a.
func (a Args) Copy() Args {
	cpy := Args{}
	for _, t := range a {
		cpy = append(cpy, t.Copy())
	}
	return cpy
}

func (a Args) String() string {
	var buf []string
	for _, t := range a {
		buf = append(buf, t.String())
	}
	return "(" + strings.Join(buf, ", ") + ")"
}

// Vars returns a set of vars that appear in a.
func (a Args) Vars() VarSet {
	vis := &VarVisitor{vars: VarSet{}}
	Walk(vis, a)
	return vis.vars
}

// NewBody returns a new Body containing the given expressions. The indices of
// the immediate expressions will be reset.
func NewBody(exprs ...*Expr) Body {
	for i, expr := range exprs {
		expr.Index = i
	}
	return Body(exprs)
}

// Append adds the expr to the body and updates the expr's index accordingly.
func (body *Body) Append(expr *Expr) {
	n := len(*body)
	expr.Index = n
	*body = append(*body, expr)
}

// Set sets the expr in the body at the specified position and updates the
// expr's index accordingly.
func (body Body) Set(expr *Expr, pos int) {
	body[pos] = expr
	expr.Index = pos
}

// Compare returns an integer indicating whether body is less than, equal to,
// or greater than other.
//
// If body is a subset of other, it is considered less than (and vice versa).
func (body Body) Compare(other Body) int {
	minLen := len(body)
	if len(other) < minLen {
		minLen = len(other)
	}
	for i := 0; i < minLen; i++ {
		if cmp := body[i].Compare(other[i]); cmp != 0 {
			return cmp
		}
	}
	if len(body) < len(other) {
		return -1
	}
	if len(other) < len(body) {
		return 1
	}
	return 0
}

// Copy returns a deep copy of body.
func (body Body) Copy() Body {
	cpy := make(Body, len(body))
	for i := range body {
		cpy[i] = body[i].Copy()
	}
	return cpy
}

// Contains returns true if this body contains the given expression.
func (body Body) Contains(x *Expr) bool {
	for _, e := range body {
		if e.Equal(x) {
			return true
		}
	}
	return false
}

// Equal returns true if this Body is equal to the other Body.
func (body Body) Equal(other Body) bool {
	return body.Compare(other) == 0
}

// Hash returns the hash code for the Body.
func (body Body) Hash() int {
	s := 0
	for _, e := range body {
		s += e.Hash()
	}
	return s
}

// IsGround returns true if all of the expressions in the Body are ground.
func (body Body) IsGround() bool {
	for _, e := range body {
		if !e.IsGround() {
			return false
		}
	}
	return true
}

// Loc returns the location of the Body in the definition.
func (body Body) Loc() *Location {
	return body[0].Location
}

func (body Body) String() string {
	var buf []string
	for _, v := range body {
		buf = append(buf, v.String())
	}
	return strings.Join(buf, "; ")
}

// Vars returns a VarSet containing variables in body. The params can be set to
// control which vars are included.
func (body Body) Vars(params VarVisitorParams) VarSet {
	vis := NewVarVisitor().WithParams(params)
	Walk(vis, body)
	return vis.Vars()
}

// NewExpr returns a new Expr object.
func NewExpr(terms interface{}) *Expr {
	return &Expr{
		Negated: false,
		Terms:   terms,
		Index:   0,
		With:    nil,
	}
}

// Complement returns a copy of this expression with the negation flag flipped.
func (expr *Expr) Complement() *Expr {
	cpy := *expr
	cpy.Negated = !cpy.Negated
	return &cpy
}

// Equal returns true if this Expr equals the other Expr.
func (expr *Expr) Equal(other *Expr) bool {
	return expr.Compare(other) == 0
}

// Compare returns an integer indicating whether expr is less than, equal to,
// or greater than other.
//
// Expressions are compared as follows:
//
// 1. Preceding expression (by Index) is always less than the other expression.
// 2. Non-negated expressions are always less than than negated expressions.
// 3. Single term expressions are always less than built-in expressions.
//
// Otherwise, the expression terms are compared normally. If both expressions
// have the same terms, the modifiers are compared.
func (expr *Expr) Compare(other *Expr) int {

	if expr == nil {
		if other == nil {
			return 0
		}
		return -1
	} else if other == nil {
		return 1
	}

	switch {
	case expr.Index < other.Index:
		return -1
	case expr.Index > other.Index:
		return 1
	}

	switch {
	case expr.Negated && !other.Negated:
		return 1
	case !expr.Negated && other.Negated:
		return -1
	}

	switch t := expr.Terms.(type) {
	case *Term:
		u, ok := other.Terms.(*Term)
		if !ok {
			return -1
		}
		if cmp := Compare(t.Value, u.Value); cmp != 0 {
			return cmp
		}
	case []*Term:
		u, ok := other.Terms.([]*Term)
		if !ok {
			return 1
		}
		if cmp := termSliceCompare(t, u); cmp != 0 {
			return cmp
		}
	}

	return withSliceCompare(expr.With, other.With)
}

// Copy returns a deep copy of expr.
func (expr *Expr) Copy() *Expr {

	cpy := *expr

	switch ts := expr.Terms.(type) {
	case []*Term:
		cpyTs := make([]*Term, len(ts))
		for i := range ts {
			cpyTs[i] = ts[i].Copy()
		}
		cpy.Terms = cpyTs
	case *Term:
		cpy.Terms = ts.Copy()
	}

	cpy.With = make([]*With, len(expr.With))
	for i := range expr.With {
		cpy.With[i] = expr.With[i].Copy()
	}

	return &cpy
}

// Hash returns the hash code of the Expr.
func (expr *Expr) Hash() int {
	s := expr.Index
	switch ts := expr.Terms.(type) {
	case []*Term:
		for _, t := range ts {
			s += t.Value.Hash()
		}
	case *Term:
		s += ts.Value.Hash()
	}
	if expr.Negated {
		s++
	}
	for _, w := range expr.With {
		s += w.Hash()
	}
	return s
}

// IncludeWith returns a copy of expr with the with modifier appended.
func (expr *Expr) IncludeWith(target *Term, value *Term) *Expr {
	cpy := *expr
	cpy.With = append(cpy.With, &With{Target: target, Value: value})
	return &cpy
}

// NoWith returns a copy of expr where the with modifier has been removed.
func (expr *Expr) NoWith() *Expr {
	expr.With = nil
	return expr
}

// IsEquality returns true if this is an equality expression.
func (expr *Expr) IsEquality() bool {
	terms, ok := expr.Terms.([]*Term)
	if !ok {
		return false
	}
	return terms[0].Value.Compare(Equality.Ref()) == 0
}

// IsAssignment returns true if this an assignment expression.
func (expr *Expr) IsAssignment() bool {
	terms, ok := expr.Terms.([]*Term)
	if !ok {
		return false
	}
	return terms[0].Value.Compare(Assign.Ref()) == 0
}

// IsCall returns true if this expression calls a function.
func (expr *Expr) IsCall() bool {
	_, ok := expr.Terms.([]*Term)
	return ok
}

// Operator returns the name of the function or built-in this expression refers
// to. If this expression is not a function call, returns nil.
func (expr *Expr) Operator() Ref {
	terms, ok := expr.Terms.([]*Term)
	if !ok || len(terms) == 0 {
		return nil
	}
	return terms[0].Value.(Ref)
}

// Operand returns the term at the zero-based pos. If the expr does not include
// at least pos+1 terms, this function returns nil.
func (expr *Expr) Operand(pos int) *Term {
	terms, ok := expr.Terms.([]*Term)
	if !ok {
		return nil
	}
	idx := pos + 1
	if idx < len(terms) {
		return terms[idx]
	}
	return nil
}

// Operands returns the built-in function operands.
func (expr *Expr) Operands() []*Term {
	terms, ok := expr.Terms.([]*Term)
	if !ok {
		return nil
	}
	return terms[1:]
}

// IsGround returns true if all of the expression terms are ground.
func (expr *Expr) IsGround() bool {
	switch ts := expr.Terms.(type) {
	case []*Term:
		for _, t := range ts[1:] {
			if !t.IsGround() {
				return false
			}
		}
	case *Term:
		return ts.IsGround()
	}
	return true
}

// SetOperator sets the expr's operator and returns the expr itself. If expr is
// not a call expr, this function will panic.
func (expr *Expr) SetOperator(term *Term) *Expr {
	expr.Terms.([]*Term)[0] = term
	return expr
}

// SetLocation sets the expr's location and returns the expr itself.
func (expr *Expr) SetLocation(loc *Location) *Expr {
	expr.Location = loc
	return expr
}

func (expr *Expr) String() string {
	var buf []string
	if expr.Negated {
		buf = append(buf, "not")
	}
	switch t := expr.Terms.(type) {
	case []*Term:
		if expr.IsEquality() {
			buf = append(buf, fmt.Sprintf("%v %v %v", t[1], Equality.Infix, t[2]))
		} else {
			buf = append(buf, Call(t).String())
		}
	case *Term:
		buf = append(buf, t.String())
	}

	for i := range expr.With {
		buf = append(buf, expr.With[i].String())
	}

	return strings.Join(buf, " ")
}

// UnmarshalJSON parses the byte array and stores the result in expr.
func (expr *Expr) UnmarshalJSON(bs []byte) error {
	v := map[string]interface{}{}
	if err := util.UnmarshalJSON(bs, &v); err != nil {
		return err
	}
	return unmarshalExpr(expr, v)
}

// Vars returns a VarSet containing variables in expr. The params can be set to
// control which vars are included.
func (expr *Expr) Vars(params VarVisitorParams) VarSet {
	vis := NewVarVisitor().WithParams(params)
	Walk(vis, expr)
	return vis.Vars()
}

// NewBuiltinExpr creates a new Expr object with the supplied terms.
// The builtin operator must be the first term.
func NewBuiltinExpr(terms ...*Term) *Expr {
	return &Expr{Terms: terms}
}

func (w *With) String() string {
	return "with " + w.Target.String() + " as " + w.Value.String()
}

// Equal returns true if this With is equals the other With.
func (w *With) Equal(other *With) bool {
	return Compare(w, other) == 0
}

// Compare returns an integer indicating whether w is less than, equal to, or
// greater than other.
func (w *With) Compare(other *With) int {
	if w == nil {
		if other == nil {
			return 0
		}
		return -1
	} else if other == nil {
		return 1
	}
	if cmp := Compare(w.Target, other.Target); cmp != 0 {
		return cmp
	}
	return Compare(w.Value, other.Value)
}

// Copy returns a deep copy of w.
func (w *With) Copy() *With {
	cpy := *w
	cpy.Value = w.Value.Copy()
	cpy.Target = w.Target.Copy()
	return &cpy
}

// Hash returns the hash code of the With.
func (w With) Hash() int {
	return w.Target.Hash() + w.Value.Hash()
}

// SetLocation sets the location on w.
func (w *With) SetLocation(loc *Location) *With {
	w.Location = loc
	return w
}

// RuleSet represents a collection of rules that produce a virtual document.
type RuleSet []*Rule

// NewRuleSet returns a new RuleSet containing the given rules.
func NewRuleSet(rules ...*Rule) RuleSet {
	rs := make(RuleSet, 0, len(rules))
	for _, rule := range rules {
		rs.Add(rule)
	}
	return rs
}

// Add inserts the rule into rs.
func (rs *RuleSet) Add(rule *Rule) {
	for _, exist := range *rs {
		if exist.Equal(rule) {
			return
		}
	}
	*rs = append(*rs, rule)
}

// Contains returns true if rs contains rule.
func (rs RuleSet) Contains(rule *Rule) bool {
	for i := range rs {
		if rs[i].Equal(rule) {
			return true
		}
	}
	return false
}

// Diff returns a new RuleSet containing rules in rs that are not in other.
func (rs RuleSet) Diff(other RuleSet) RuleSet {
	result := NewRuleSet()
	for i := range rs {
		if !other.Contains(rs[i]) {
			result.Add(rs[i])
		}
	}
	return result
}

// Equal returns true if rs equals other.
func (rs RuleSet) Equal(other RuleSet) bool {
	return len(rs.Diff(other)) == 0 && len(other.Diff(rs)) == 0
}

// Merge returns a ruleset containing the union of rules from rs an other.
func (rs RuleSet) Merge(other RuleSet) RuleSet {
	result := NewRuleSet()
	for i := range rs {
		result.Add(rs[i])
	}
	for i := range other {
		result.Add(other[i])
	}
	return result
}

func (rs RuleSet) String() string {
	buf := make([]string, 0, len(rs))
	for _, rule := range rs {
		buf = append(buf, rule.String())
	}
	return "{" + strings.Join(buf, ", ") + "}"
}

type ruleSlice []*Rule

func (s ruleSlice) Less(i, j int) bool { return Compare(s[i], s[j]) < 0 }
func (s ruleSlice) Swap(i, j int)      { x := s[i]; s[i] = s[j]; s[j] = x }
func (s ruleSlice) Len() int           { return len(s) }
