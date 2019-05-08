// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"
	"io"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

// Op defines the types of tracing events.
type Op string

const (
	// EnterOp is emitted when a new query is about to be evaluated.
	EnterOp Op = "Enter"

	// ExitOp is emitted when a query has evaluated to true.
	ExitOp Op = "Exit"

	// EvalOp is emitted when an expression is about to be evaluated.
	EvalOp Op = "Eval"

	// RedoOp is emitted when an expression, rule, or query is being re-evaluated.
	RedoOp Op = "Redo"

	// SaveOp is emitted when an expression is saved instead of evaluated
	// during partial evaluation.
	SaveOp Op = "Save"

	// FailOp is emitted when an expression evaluates to false.
	FailOp Op = "Fail"

	// NoteOp is emitted when an expression invokes a tracing built-in function.
	NoteOp Op = "Note"

	// IndexOp is emitted during an expression evaluation to represent lookup
	// matches.
	IndexOp Op = "Index"
)

// Event contains state associated with a tracing event.
type Event struct {
	Op       Op            // Identifies type of event.
	Node     interface{}   // Contains AST node relevant to the event.
	QueryID  uint64        // Identifies the query this event belongs to.
	ParentID uint64        // Identifies the parent query this event belongs to.
	Locals   *ast.ValueMap // Contains local variable bindings from the query context.
	Message  string        // Contains message for Note events.
}

// HasRule returns true if the Event contains an ast.Rule.
func (evt *Event) HasRule() bool {
	_, ok := evt.Node.(*ast.Rule)
	return ok
}

// HasBody returns true if the Event contains an ast.Body.
func (evt *Event) HasBody() bool {
	_, ok := evt.Node.(ast.Body)
	return ok
}

// HasExpr returns true if the Event contains an ast.Expr.
func (evt *Event) HasExpr() bool {
	_, ok := evt.Node.(*ast.Expr)
	return ok
}

// Equal returns true if this event is equal to the other event.
func (evt *Event) Equal(other *Event) bool {
	if evt.Op != other.Op {
		return false
	}
	if evt.QueryID != other.QueryID {
		return false
	}
	if evt.ParentID != other.ParentID {
		return false
	}
	if !evt.equalNodes(other) {
		return false
	}
	return evt.Locals.Equal(other.Locals)
}

func (evt *Event) String() string {
	return fmt.Sprintf("%v %v %v (qid=%v, pqid=%v)", evt.Op, evt.Node, evt.Locals, evt.QueryID, evt.ParentID)
}

func (evt *Event) equalNodes(other *Event) bool {
	switch a := evt.Node.(type) {
	case ast.Body:
		if b, ok := other.Node.(ast.Body); ok {
			return a.Equal(b)
		}
	case *ast.Rule:
		if b, ok := other.Node.(*ast.Rule); ok {
			return a.Equal(b)
		}
	case *ast.Expr:
		if b, ok := other.Node.(*ast.Expr); ok {
			return a.Equal(b)
		}
	case nil:
		return other.Node == nil
	}
	return false
}

// Tracer defines the interface for tracing in the top-down evaluation engine.
type Tracer interface {
	Enabled() bool
	Trace(*Event)
}

// BufferTracer implements the Tracer interface by simply buffering all events
// received.
type BufferTracer []*Event

// NewBufferTracer returns a new BufferTracer.
func NewBufferTracer() *BufferTracer {
	return &BufferTracer{}
}

// Enabled always returns true if the BufferTracer is instantiated.
func (b *BufferTracer) Enabled() bool {
	if b == nil {
		return false
	}
	return true
}

// Trace adds the event to the buffer.
func (b *BufferTracer) Trace(evt *Event) {
	*b = append(*b, evt)
}

// PrettyTrace pretty prints the trace to the writer.
func PrettyTrace(w io.Writer, trace []*Event) {
	depths := depths{}
	for _, event := range trace {
		depth := depths.GetOrSet(event.QueryID, event.ParentID)
		fmt.Fprintln(w, formatEvent(event, depth))
	}
}

func formatEvent(event *Event, depth int) string {
	padding := formatEventPadding(event, depth)
	if event.Op == NoteOp {
		return fmt.Sprintf("%v%v %q", padding, event.Op, event.Message)
	}
	return fmt.Sprintf("%v%v %v", padding, event.Op, event.Node)
}

func formatEventPadding(event *Event, depth int) string {
	spaces := formatEventSpaces(event, depth)
	padding := ""
	if spaces > 1 {
		padding += strings.Repeat("| ", spaces-1)
	}
	return padding
}

func formatEventSpaces(event *Event, depth int) int {
	switch event.Op {
	case EnterOp:
		return depth
	case RedoOp:
		if _, ok := event.Node.(*ast.Expr); !ok {
			return depth
		}
	}
	return depth + 1
}

// depths is a helper for computing the depth of an event. Events within the
// same query all have the same depth. The depth of query is
// depth(parent(query))+1.
type depths map[uint64]int

func (ds depths) GetOrSet(qid uint64, pqid uint64) int {
	depth := ds[qid]
	if depth == 0 {
		depth = ds[pqid]
		depth++
		ds[qid] = depth
	}
	return depth
}

func builtinTrace(bctx BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {

	str, err := builtins.StringOperand(args[0].Value, 1)
	if err != nil {
		return handleBuiltinErr(ast.Trace.Name, bctx.Location, err)
	}

	if bctx.Tracer == nil || !bctx.Tracer.Enabled() {
		return iter(ast.BooleanTerm(true))
	}

	evt := &Event{
		Op:       NoteOp,
		QueryID:  bctx.QueryID,
		ParentID: bctx.ParentID,
		Message:  string(str),
	}
	bctx.Tracer.Trace(evt)

	return iter(ast.BooleanTerm(true))
}

func init() {
	RegisterBuiltinFunc(ast.Trace.Name, builtinTrace)
}
