// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/open-policy-agent/opa/util"
)

// RuleIndex defines the interface for rule indices.
type RuleIndex interface {

	// Build tries to construct an index for the given rules. If the index was
	// constructed, ok is true, otherwise false.
	Build(rules []*Rule) (ok bool)

	// Lookup searches the index for rules that will match the provided
	// resolver. If the resolver returns an error, it is returned via err.
	Lookup(resolver ValueResolver) (result *IndexResult, err error)
}

// IndexResult contains the result of an index lookup.
type IndexResult struct {
	Kind    DocKind
	Rules   []*Rule
	Else    map[*Rule][]*Rule
	Default *Rule
}

// NewIndexResult returns a new IndexResult object.
func NewIndexResult(kind DocKind) *IndexResult {
	return &IndexResult{
		Kind: kind,
		Else: map[*Rule][]*Rule{},
	}
}

// Empty returns true if there are no rules to evaluate.
func (ir *IndexResult) Empty() bool {
	return len(ir.Rules) == 0 && ir.Default == nil
}

type baseDocEqIndex struct {
	isVirtual   func(Ref) bool
	root        *trieNode
	defaultRule *Rule
	kind        DocKind
}

func newBaseDocEqIndex(isVirtual func(Ref) bool) *baseDocEqIndex {
	return &baseDocEqIndex{
		isVirtual: isVirtual,
		root:      newTrieNodeImpl(),
	}
}

func (i *baseDocEqIndex) Build(rules []*Rule) bool {

	if len(rules) == 0 {
		return false
	}

	i.kind = rules[0].Head.DocKind()
	refs := make(refValueIndex, len(rules))

	// freq is map[ref]int where the values represent the frequency of the
	// ref/key.
	freq := util.NewHashMap(func(a, b util.T) bool {
		r1, r2 := a.(Ref), b.(Ref)
		return r1.Equal(r2)
	}, func(x util.T) int {
		return x.(Ref).Hash()
	})

	// Build refs and freq maps
	for idx := range rules {

		WalkRules(rules[idx], func(rule *Rule) bool {

			if rule.Default {
				// Compiler guarantees that only one default will be defined per path.
				i.defaultRule = rule
				return false
			}

			for _, expr := range rule.Body {
				ref, value, ok := i.getRefAndValue(expr)
				if ok {
					refs.Insert(rule, ref, value)
					count, ok := freq.Get(ref)
					if !ok {
						count = 0
					}
					count = count.(int) + 1
					freq.Put(ref, count)
				}
			}

			return false
		})
	}

	// Sort by frequency
	type refCountPair struct {
		ref   Ref
		count int
	}

	sorted := make([]refCountPair, 0, freq.Len())
	freq.Iter(func(k, v util.T) bool {
		ref, count := k.(Ref), v.(int)
		sorted = append(sorted, refCountPair{ref, count})
		return false
	})

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count > sorted[j].count {
			return true
		}
		return false
	})

	// Build trie
	for idx := range rules {

		var prio int

		WalkRules(rules[idx], func(rule *Rule) bool {

			if rule.Default {
				return false
			}

			node := i.root

			if refs := refs[rule]; refs != nil {
				for _, pair := range sorted {
					value := refs.Get(pair.ref)
					node = node.Insert(pair.ref, value)
				}
			}

			// Insert rule into trie with (insertion order, priority order)
			// tuple. Retaining the insertion order allows us to return rules
			// in the order they were passed to this function.
			node.rules = append(node.rules, &ruleNode{[...]int{idx, prio}, rule})
			prio++
			return false
		})

	}

	return true
}

func (i *baseDocEqIndex) Lookup(resolver ValueResolver) (*IndexResult, error) {

	tr := newTrieTraversalResult()

	err := i.root.Traverse(resolver, tr)
	if err != nil {
		return nil, err
	}

	result := NewIndexResult(i.kind)
	result.Default = i.defaultRule
	result.Rules = make([]*Rule, 0, len(tr.ordering))

	for _, pos := range tr.ordering {
		sort.Slice(tr.unordered[pos], func(i, j int) bool {
			return tr.unordered[pos][i].prio[1] < tr.unordered[pos][j].prio[1]
		})
		nodes := tr.unordered[pos]
		root := nodes[0].rule
		result.Rules = append(result.Rules, root)
		if len(nodes) > 1 {
			result.Else[root] = make([]*Rule, len(nodes)-1)
			for i := 1; i < len(nodes); i++ {
				result.Else[root][i-1] = nodes[i].rule
			}
		}
	}

	return result, nil
}

func (i *baseDocEqIndex) getRefAndValue(expr *Expr) (Ref, Value, bool) {

	if !expr.IsEquality() || expr.Negated {
		return nil, nil, false
	}

	a, b := expr.Operand(0), expr.Operand(1)

	if ref, value, ok := i.getRefAndValueFromTerms(a, b); ok {
		return ref, value, true
	}

	return i.getRefAndValueFromTerms(b, a)
}

func (i *baseDocEqIndex) getRefAndValueFromTerms(a, b *Term) (Ref, Value, bool) {

	ref, ok := a.Value.(Ref)
	if !ok {
		return nil, nil, false
	}

	if !RootDocumentNames.Contains(ref[0]) {
		return nil, nil, false
	}

	if i.isVirtual(ref) {
		return nil, nil, false
	}

	if ref.IsNested() || !ref.IsGround() {
		return nil, nil, false
	}

	switch b := b.Value.(type) {
	case Null, Boolean, Number, String, Var:
		return ref, b, true
	case Array:
		stop := false
		first := true
		vis := NewGenericVisitor(func(x interface{}) bool {
			if first {
				first = false
				return false
			}
			switch x.(type) {
			// No nested structures or values that require evaluation (other than var).
			case Array, Object, Set, *ArrayComprehension, *ObjectComprehension, *SetComprehension, Ref:
				stop = true
			}
			return stop
		})
		Walk(vis, b)
		if !stop {
			return ref, b, true
		}
	}

	return nil, nil, false
}

type refValueIndex map[*Rule]*ValueMap

func (m refValueIndex) Insert(rule *Rule, ref Ref, value Value) {
	vm, ok := m[rule]
	if !ok {
		vm = NewValueMap()
		m[rule] = vm
	}
	vm.Put(ref, value)
}

type trieWalker interface {
	Do(x interface{}) trieWalker
}

type trieTraversalResult struct {
	unordered map[int][]*ruleNode
	ordering  []int
}

func newTrieTraversalResult() *trieTraversalResult {
	return &trieTraversalResult{
		unordered: map[int][]*ruleNode{},
	}
}

func (tr *trieTraversalResult) Add(node *ruleNode) {
	root := node.prio[0]
	nodes, ok := tr.unordered[root]
	if !ok {
		tr.ordering = append(tr.ordering, root)
	}
	tr.unordered[root] = append(nodes, node)
}

type trieNode struct {
	ref       Ref
	next      *trieNode
	any       *trieNode
	undefined *trieNode
	scalars   map[Value]*trieNode
	array     *trieNode
	rules     []*ruleNode
}

type ruleNode struct {
	prio [2]int
	rule *Rule
}

func newTrieNodeImpl() *trieNode {
	return &trieNode{
		scalars: map[Value]*trieNode{},
	}
}

func (node *trieNode) Do(walker trieWalker) {
	next := walker.Do(node)
	if next == nil {
		return
	}
	if node.next != nil {
		node.next.Do(next)
		return
	}
	if node.any != nil {
		node.any.Do(next)
	}
	if node.undefined != nil {
		node.undefined.Do(next)
	}
	for _, child := range node.scalars {
		child.Do(next)
	}
	if node.array != nil {
		node.array.Do(next)
	}
}

func (node *trieNode) Insert(ref Ref, value Value) *trieNode {

	if node.next == nil {
		node.next = newTrieNodeImpl()
		node.next.ref = ref
	}

	return node.next.insertValue(value)
}

func (node *trieNode) Traverse(resolver ValueResolver, tr *trieTraversalResult) error {

	if node == nil {
		return nil
	}

	for i := range node.rules {
		tr.Add(node.rules[i])
	}

	return node.next.traverse(resolver, tr)
}

func (node *trieNode) insertValue(value Value) *trieNode {

	switch value := value.(type) {
	case nil:
		if node.undefined == nil {
			node.undefined = newTrieNodeImpl()
		}
		return node.undefined
	case Var:
		if node.any == nil {
			node.any = newTrieNodeImpl()
		}
		return node.any
	case Null, Boolean, Number, String:
		child, ok := node.scalars[value]
		if !ok {
			child = newTrieNodeImpl()
			node.scalars[value] = child
		}
		return child
	case Array:
		if node.array == nil {
			node.array = newTrieNodeImpl()
		}
		return node.array.insertArray(value)
	}

	panic("illegal value")
}

func (node *trieNode) insertArray(arr Array) *trieNode {

	if len(arr) == 0 {
		return node
	}

	switch head := arr[0].Value.(type) {
	case Var:
		if node.any == nil {
			node.any = newTrieNodeImpl()
		}
		return node.any.insertArray(arr[1:])
	case Null, Boolean, Number, String:
		child, ok := node.scalars[head]
		if !ok {
			child = newTrieNodeImpl()
			node.scalars[head] = child
		}
		return child.insertArray(arr[1:])
	}

	panic("illegal value")
}

func (node *trieNode) traverse(resolver ValueResolver, tr *trieTraversalResult) error {

	if node == nil {
		return nil
	}

	v, err := resolver.Resolve(node.ref)
	if err != nil {
		return err
	}

	if node.undefined != nil {
		node.undefined.Traverse(resolver, tr)
	}

	if v == nil {
		return nil
	}

	if node.any != nil {
		node.any.Traverse(resolver, tr)
	}

	return node.traverseValue(resolver, tr, v)
}

func (node *trieNode) traverseValue(resolver ValueResolver, tr *trieTraversalResult, value Value) error {

	switch value := value.(type) {
	case Array:
		if node.array == nil {
			return nil
		}
		return node.array.traverseArray(resolver, tr, value)

	case Null, Boolean, Number, String:
		child, ok := node.scalars[value]
		if !ok {
			return nil
		}
		return child.Traverse(resolver, tr)
	}

	return nil
}

func (node *trieNode) traverseArray(resolver ValueResolver, tr *trieTraversalResult, arr Array) error {

	if len(arr) == 0 {
		if node.next != nil || len(node.rules) > 0 {
			return node.Traverse(resolver, tr)
		}
		return nil
	}

	head := arr[0].Value

	if !IsScalar(head) {
		return nil
	}

	if node.any != nil {
		node.any.traverseArray(resolver, tr, arr[1:])
	}

	child, ok := node.scalars[head]
	if !ok {
		return nil
	}

	return child.traverseArray(resolver, tr, arr[1:])
}

type triePrinter struct {
	depth int
	w     io.Writer
}

func (p triePrinter) Do(x interface{}) trieWalker {
	padding := strings.Repeat(" ", p.depth)
	fmt.Fprintf(p.w, "%v%v\n", padding, x)
	p.depth++
	return p
}

func printTrie(w io.Writer, trie *trieNode) {
	pp := triePrinter{0, w}
	trie.Do(pp)
}
