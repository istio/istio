package ast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// commentsKey is the global map key for the comments slice.
	commentsKey = "comments"

	// filenameKey is the global map key for the filename.
	filenameKey = "filename"
)

type program struct {
	buf      []interface{}
	comments interface{}
}

type ruleExt struct {
	loc  *Location
	term *Term
	body Body
}

// currentLocation converts the parser context to a Location object.
func currentLocation(c *current) *Location {
	return NewLocation(c.text, c.globalStore[filenameKey].(string), c.pos.line, c.pos.col)
}

func ifaceSliceToByteSlice(i interface{}) []byte {
	var buf bytes.Buffer
	for _, x := range i.([]interface{}) {
		buf.Write(x.([]byte))
	}
	return buf.Bytes()
}

func ifacesToBody(i interface{}, a ...interface{}) Body {
	var buf Body
	buf = append(buf, i.(*Expr))
	for _, s := range a {
		expr := s.([]interface{})[3].(*Expr)
		buf = append(buf, expr)
	}
	return buf
}

func makeObject(head interface{}, tail interface{}, loc *Location) (*Term, error) {
	obj := ObjectTerm()
	obj.Location = loc

	// Empty object.
	if head == nil {
		return obj, nil
	}

	// Object definition above describes the "head" structure. We only care about the "Key" and "Term" elements.
	headSlice := head.([]interface{})
	obj.Value = append(obj.Value.(Object), Item(headSlice[0].(*Term), headSlice[len(headSlice)-1].(*Term)))

	// Non-empty object, remaining key/value pairs.
	tailSlice := tail.([]interface{})
	for _, v := range tailSlice {
		s := v.([]interface{})
		// Object definition above describes the "tail" structure. We only care about the "Key" and "Term" elements.
		obj.Value = append(obj.Value.(Object), Item(s[3].(*Term), s[len(s)-1].(*Term)))
	}

	return obj, nil
}

func makeArray(head interface{}, tail interface{}, loc *Location) (*Term, error) {
	arr := ArrayTerm()
	arr.Location = loc

	// Empty array.
	if head == nil {
		return arr, nil
	}

	// Non-empty array, first element.
	arr.Value = append(arr.Value.(Array), head.(*Term))

	// Non-empty array, remaining elements.
	tailSlice := tail.([]interface{})
	for _, v := range tailSlice {
		s := v.([]interface{})
		// Array definition above describes the "tail" structure. We only care about the "Term" elements.
		arr.Value = append(arr.Value.(Array), s[len(s)-1].(*Term))
	}

	return arr, nil
}

func makeArgs(head interface{}, tail interface{}, loc *Location) (Args, error) {
	args := Args{}
	if head == nil {
		return nil, nil
	}
	args = append(args, head.(*Term))
	tailSlice := tail.([]interface{})
	for _, v := range tailSlice {
		s := v.([]interface{})
		args = append(args, s[len(s)-1].(*Term))
	}
	return args, nil
}

func makeInfixCallExpr(operator interface{}, args interface{}, output interface{}) (*Expr, error) {
	expr := &Expr{}
	a := args.(Args)
	terms := make([]*Term, len(a)+2)
	terms[0] = operator.(*Term)
	dst := terms[1:]
	for i := 0; i < len(a); i++ {
		dst[i] = a[i]
	}
	terms[len(terms)-1] = output.(*Term)
	expr.Terms = terms
	expr.Infix = true
	return expr, nil
}

var g = &grammar{
	rules: []*rule{
		{
			name: "Program",
			pos:  position{line: 125, col: 1, offset: 3203},
			expr: &actionExpr{
				pos: position{line: 125, col: 12, offset: 3214},
				run: (*parser).callonProgram1,
				expr: &seqExpr{
					pos: position{line: 125, col: 12, offset: 3214},
					exprs: []interface{}{
						&ruleRefExpr{
							pos:  position{line: 125, col: 12, offset: 3214},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 125, col: 14, offset: 3216},
							label: "vals",
							expr: &zeroOrOneExpr{
								pos: position{line: 125, col: 19, offset: 3221},
								expr: &seqExpr{
									pos: position{line: 125, col: 20, offset: 3222},
									exprs: []interface{}{
										&labeledExpr{
											pos:   position{line: 125, col: 20, offset: 3222},
											label: "head",
											expr: &ruleRefExpr{
												pos:  position{line: 125, col: 25, offset: 3227},
												name: "Stmt",
											},
										},
										&labeledExpr{
											pos:   position{line: 125, col: 30, offset: 3232},
											label: "tail",
											expr: &zeroOrMoreExpr{
												pos: position{line: 125, col: 35, offset: 3237},
												expr: &seqExpr{
													pos: position{line: 125, col: 36, offset: 3238},
													exprs: []interface{}{
														&ruleRefExpr{
															pos:  position{line: 125, col: 36, offset: 3238},
															name: "ws",
														},
														&ruleRefExpr{
															pos:  position{line: 125, col: 39, offset: 3241},
															name: "Stmt",
														},
													},
												},
											},
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 125, col: 48, offset: 3250},
							name: "_",
						},
						&ruleRefExpr{
							pos:  position{line: 125, col: 50, offset: 3252},
							name: "EOF",
						},
					},
				},
			},
		},
		{
			name: "Stmt",
			pos:  position{line: 143, col: 1, offset: 3626},
			expr: &actionExpr{
				pos: position{line: 143, col: 9, offset: 3634},
				run: (*parser).callonStmt1,
				expr: &labeledExpr{
					pos:   position{line: 143, col: 9, offset: 3634},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 143, col: 14, offset: 3639},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 143, col: 14, offset: 3639},
								name: "Package",
							},
							&ruleRefExpr{
								pos:  position{line: 143, col: 24, offset: 3649},
								name: "Import",
							},
							&ruleRefExpr{
								pos:  position{line: 143, col: 33, offset: 3658},
								name: "Rules",
							},
							&ruleRefExpr{
								pos:  position{line: 143, col: 41, offset: 3666},
								name: "Body",
							},
							&ruleRefExpr{
								pos:  position{line: 143, col: 48, offset: 3673},
								name: "Comment",
							},
						},
					},
				},
			},
		},
		{
			name: "Package",
			pos:  position{line: 147, col: 1, offset: 3707},
			expr: &actionExpr{
				pos: position{line: 147, col: 12, offset: 3718},
				run: (*parser).callonPackage1,
				expr: &seqExpr{
					pos: position{line: 147, col: 12, offset: 3718},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 147, col: 12, offset: 3718},
							val:        "package",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 147, col: 22, offset: 3728},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 147, col: 25, offset: 3731},
							label: "val",
							expr: &choiceExpr{
								pos: position{line: 147, col: 30, offset: 3736},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 147, col: 30, offset: 3736},
										name: "Ref",
									},
									&ruleRefExpr{
										pos:  position{line: 147, col: 36, offset: 3742},
										name: "Var",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Import",
			pos:  position{line: 181, col: 1, offset: 5058},
			expr: &actionExpr{
				pos: position{line: 181, col: 11, offset: 5068},
				run: (*parser).callonImport1,
				expr: &seqExpr{
					pos: position{line: 181, col: 11, offset: 5068},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 181, col: 11, offset: 5068},
							val:        "import",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 181, col: 20, offset: 5077},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 181, col: 23, offset: 5080},
							label: "path",
							expr: &choiceExpr{
								pos: position{line: 181, col: 29, offset: 5086},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 181, col: 29, offset: 5086},
										name: "Ref",
									},
									&ruleRefExpr{
										pos:  position{line: 181, col: 35, offset: 5092},
										name: "Var",
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 181, col: 40, offset: 5097},
							label: "alias",
							expr: &zeroOrOneExpr{
								pos: position{line: 181, col: 46, offset: 5103},
								expr: &seqExpr{
									pos: position{line: 181, col: 47, offset: 5104},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 181, col: 47, offset: 5104},
											name: "ws",
										},
										&litMatcher{
											pos:        position{line: 181, col: 50, offset: 5107},
											val:        "as",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 181, col: 55, offset: 5112},
											name: "ws",
										},
										&ruleRefExpr{
											pos:  position{line: 181, col: 58, offset: 5115},
											name: "Var",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Rules",
			pos:  position{line: 197, col: 1, offset: 5565},
			expr: &choiceExpr{
				pos: position{line: 197, col: 10, offset: 5574},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 197, col: 10, offset: 5574},
						name: "DefaultRules",
					},
					&ruleRefExpr{
						pos:  position{line: 197, col: 25, offset: 5589},
						name: "NormalRules",
					},
				},
			},
		},
		{
			name: "DefaultRules",
			pos:  position{line: 199, col: 1, offset: 5602},
			expr: &actionExpr{
				pos: position{line: 199, col: 17, offset: 5618},
				run: (*parser).callonDefaultRules1,
				expr: &seqExpr{
					pos: position{line: 199, col: 17, offset: 5618},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 199, col: 17, offset: 5618},
							val:        "default",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 199, col: 27, offset: 5628},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 199, col: 30, offset: 5631},
							label: "name",
							expr: &ruleRefExpr{
								pos:  position{line: 199, col: 35, offset: 5636},
								name: "Var",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 199, col: 39, offset: 5640},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 199, col: 41, offset: 5642},
							val:        "=",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 199, col: 45, offset: 5646},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 199, col: 47, offset: 5648},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 199, col: 53, offset: 5654},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "NormalRules",
			pos:  position{line: 242, col: 1, offset: 6623},
			expr: &actionExpr{
				pos: position{line: 242, col: 16, offset: 6638},
				run: (*parser).callonNormalRules1,
				expr: &seqExpr{
					pos: position{line: 242, col: 16, offset: 6638},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 242, col: 16, offset: 6638},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 242, col: 21, offset: 6643},
								name: "RuleHead",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 242, col: 30, offset: 6652},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 242, col: 32, offset: 6654},
							label: "b",
							expr: &seqExpr{
								pos: position{line: 242, col: 35, offset: 6657},
								exprs: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 242, col: 35, offset: 6657},
										name: "NonEmptyBraceEnclosedBody",
									},
									&zeroOrMoreExpr{
										pos: position{line: 242, col: 61, offset: 6683},
										expr: &seqExpr{
											pos: position{line: 242, col: 63, offset: 6685},
											exprs: []interface{}{
												&ruleRefExpr{
													pos:  position{line: 242, col: 63, offset: 6685},
													name: "_",
												},
												&ruleRefExpr{
													pos:  position{line: 242, col: 65, offset: 6687},
													name: "RuleExt",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "RuleHead",
			pos:  position{line: 298, col: 1, offset: 8017},
			expr: &actionExpr{
				pos: position{line: 298, col: 13, offset: 8029},
				run: (*parser).callonRuleHead1,
				expr: &seqExpr{
					pos: position{line: 298, col: 13, offset: 8029},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 298, col: 13, offset: 8029},
							label: "name",
							expr: &ruleRefExpr{
								pos:  position{line: 298, col: 18, offset: 8034},
								name: "Var",
							},
						},
						&labeledExpr{
							pos:   position{line: 298, col: 22, offset: 8038},
							label: "args",
							expr: &zeroOrOneExpr{
								pos: position{line: 298, col: 27, offset: 8043},
								expr: &seqExpr{
									pos: position{line: 298, col: 29, offset: 8045},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 298, col: 29, offset: 8045},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 298, col: 31, offset: 8047},
											val:        "(",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 35, offset: 8051},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 37, offset: 8053},
											name: "Args",
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 42, offset: 8058},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 298, col: 44, offset: 8060},
											val:        ")",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 48, offset: 8064},
											name: "_",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 298, col: 53, offset: 8069},
							label: "key",
							expr: &zeroOrOneExpr{
								pos: position{line: 298, col: 57, offset: 8073},
								expr: &seqExpr{
									pos: position{line: 298, col: 59, offset: 8075},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 298, col: 59, offset: 8075},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 298, col: 61, offset: 8077},
											val:        "[",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 65, offset: 8081},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 67, offset: 8083},
											name: "Term",
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 72, offset: 8088},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 298, col: 74, offset: 8090},
											val:        "]",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 78, offset: 8094},
											name: "_",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 298, col: 83, offset: 8099},
							label: "value",
							expr: &zeroOrOneExpr{
								pos: position{line: 298, col: 89, offset: 8105},
								expr: &seqExpr{
									pos: position{line: 298, col: 91, offset: 8107},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 298, col: 91, offset: 8107},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 298, col: 93, offset: 8109},
											val:        "=",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 97, offset: 8113},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 298, col: 99, offset: 8115},
											name: "Term",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Args",
			pos:  position{line: 341, col: 1, offset: 9323},
			expr: &actionExpr{
				pos: position{line: 341, col: 9, offset: 9331},
				run: (*parser).callonArgs1,
				expr: &seqExpr{
					pos: position{line: 341, col: 9, offset: 9331},
					exprs: []interface{}{
						&ruleRefExpr{
							pos:  position{line: 341, col: 9, offset: 9331},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 341, col: 11, offset: 9333},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 341, col: 16, offset: 9338},
								name: "Term",
							},
						},
						&labeledExpr{
							pos:   position{line: 341, col: 21, offset: 9343},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 341, col: 26, offset: 9348},
								expr: &seqExpr{
									pos: position{line: 341, col: 27, offset: 9349},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 341, col: 27, offset: 9349},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 341, col: 29, offset: 9351},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 341, col: 33, offset: 9355},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 341, col: 35, offset: 9357},
											name: "Term",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 341, col: 42, offset: 9364},
							name: "_",
						},
						&zeroOrOneExpr{
							pos: position{line: 341, col: 44, offset: 9366},
							expr: &litMatcher{
								pos:        position{line: 341, col: 44, offset: 9366},
								val:        ",",
								ignoreCase: false,
							},
						},
						&ruleRefExpr{
							pos:  position{line: 341, col: 49, offset: 9371},
							name: "_",
						},
					},
				},
			},
		},
		{
			name: "Else",
			pos:  position{line: 345, col: 1, offset: 9427},
			expr: &actionExpr{
				pos: position{line: 345, col: 9, offset: 9435},
				run: (*parser).callonElse1,
				expr: &seqExpr{
					pos: position{line: 345, col: 9, offset: 9435},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 345, col: 9, offset: 9435},
							val:        "else",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 345, col: 16, offset: 9442},
							label: "val",
							expr: &zeroOrOneExpr{
								pos: position{line: 345, col: 20, offset: 9446},
								expr: &seqExpr{
									pos: position{line: 345, col: 22, offset: 9448},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 345, col: 22, offset: 9448},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 345, col: 24, offset: 9450},
											val:        "=",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 345, col: 28, offset: 9454},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 345, col: 30, offset: 9456},
											name: "Term",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 345, col: 38, offset: 9464},
							label: "b",
							expr: &seqExpr{
								pos: position{line: 345, col: 42, offset: 9468},
								exprs: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 345, col: 42, offset: 9468},
										name: "_",
									},
									&ruleRefExpr{
										pos:  position{line: 345, col: 44, offset: 9470},
										name: "NonEmptyBraceEnclosedBody",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "RuleDup",
			pos:  position{line: 360, col: 1, offset: 9822},
			expr: &actionExpr{
				pos: position{line: 360, col: 12, offset: 9833},
				run: (*parser).callonRuleDup1,
				expr: &labeledExpr{
					pos:   position{line: 360, col: 12, offset: 9833},
					label: "b",
					expr: &ruleRefExpr{
						pos:  position{line: 360, col: 14, offset: 9835},
						name: "NonEmptyBraceEnclosedBody",
					},
				},
			},
		},
		{
			name: "RuleExt",
			pos:  position{line: 364, col: 1, offset: 9931},
			expr: &choiceExpr{
				pos: position{line: 364, col: 12, offset: 9942},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 364, col: 12, offset: 9942},
						name: "Else",
					},
					&ruleRefExpr{
						pos:  position{line: 364, col: 19, offset: 9949},
						name: "RuleDup",
					},
				},
			},
		},
		{
			name: "Body",
			pos:  position{line: 366, col: 1, offset: 9958},
			expr: &choiceExpr{
				pos: position{line: 366, col: 9, offset: 9966},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 366, col: 9, offset: 9966},
						name: "NonWhitespaceBody",
					},
					&ruleRefExpr{
						pos:  position{line: 366, col: 29, offset: 9986},
						name: "BraceEnclosedBody",
					},
				},
			},
		},
		{
			name: "NonEmptyBraceEnclosedBody",
			pos:  position{line: 368, col: 1, offset: 10005},
			expr: &actionExpr{
				pos: position{line: 368, col: 30, offset: 10034},
				run: (*parser).callonNonEmptyBraceEnclosedBody1,
				expr: &seqExpr{
					pos: position{line: 368, col: 30, offset: 10034},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 368, col: 30, offset: 10034},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 368, col: 34, offset: 10038},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 368, col: 36, offset: 10040},
							label: "val",
							expr: &zeroOrOneExpr{
								pos: position{line: 368, col: 40, offset: 10044},
								expr: &ruleRefExpr{
									pos:  position{line: 368, col: 40, offset: 10044},
									name: "WhitespaceBody",
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 368, col: 56, offset: 10060},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 368, col: 58, offset: 10062},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "BraceEnclosedBody",
			pos:  position{line: 375, col: 1, offset: 10157},
			expr: &actionExpr{
				pos: position{line: 375, col: 22, offset: 10178},
				run: (*parser).callonBraceEnclosedBody1,
				expr: &seqExpr{
					pos: position{line: 375, col: 22, offset: 10178},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 375, col: 22, offset: 10178},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 375, col: 26, offset: 10182},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 375, col: 28, offset: 10184},
							label: "val",
							expr: &zeroOrOneExpr{
								pos: position{line: 375, col: 32, offset: 10188},
								expr: &ruleRefExpr{
									pos:  position{line: 375, col: 32, offset: 10188},
									name: "WhitespaceBody",
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 375, col: 48, offset: 10204},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 375, col: 50, offset: 10206},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "WhitespaceBody",
			pos:  position{line: 389, col: 1, offset: 10558},
			expr: &actionExpr{
				pos: position{line: 389, col: 19, offset: 10576},
				run: (*parser).callonWhitespaceBody1,
				expr: &seqExpr{
					pos: position{line: 389, col: 19, offset: 10576},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 389, col: 19, offset: 10576},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 389, col: 24, offset: 10581},
								name: "Literal",
							},
						},
						&labeledExpr{
							pos:   position{line: 389, col: 32, offset: 10589},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 389, col: 37, offset: 10594},
								expr: &seqExpr{
									pos: position{line: 389, col: 38, offset: 10595},
									exprs: []interface{}{
										&zeroOrMoreExpr{
											pos: position{line: 389, col: 38, offset: 10595},
											expr: &charClassMatcher{
												pos:        position{line: 389, col: 38, offset: 10595},
												val:        "[ \\t]",
												chars:      []rune{' ', '\t'},
												ignoreCase: false,
												inverted:   false,
											},
										},
										&choiceExpr{
											pos: position{line: 389, col: 46, offset: 10603},
											alternatives: []interface{}{
												&seqExpr{
													pos: position{line: 389, col: 47, offset: 10604},
													exprs: []interface{}{
														&litMatcher{
															pos:        position{line: 389, col: 47, offset: 10604},
															val:        ";",
															ignoreCase: false,
														},
														&zeroOrOneExpr{
															pos: position{line: 389, col: 51, offset: 10608},
															expr: &ruleRefExpr{
																pos:  position{line: 389, col: 51, offset: 10608},
																name: "Comment",
															},
														},
													},
												},
												&seqExpr{
													pos: position{line: 389, col: 64, offset: 10621},
													exprs: []interface{}{
														&zeroOrOneExpr{
															pos: position{line: 389, col: 64, offset: 10621},
															expr: &ruleRefExpr{
																pos:  position{line: 389, col: 64, offset: 10621},
																name: "Comment",
															},
														},
														&charClassMatcher{
															pos:        position{line: 389, col: 73, offset: 10630},
															val:        "[\\r\\n]",
															chars:      []rune{'\r', '\n'},
															ignoreCase: false,
															inverted:   false,
														},
													},
												},
											},
										},
										&ruleRefExpr{
											pos:  position{line: 389, col: 82, offset: 10639},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 389, col: 84, offset: 10641},
											name: "Literal",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "NonWhitespaceBody",
			pos:  position{line: 395, col: 1, offset: 10830},
			expr: &actionExpr{
				pos: position{line: 395, col: 22, offset: 10851},
				run: (*parser).callonNonWhitespaceBody1,
				expr: &seqExpr{
					pos: position{line: 395, col: 22, offset: 10851},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 395, col: 22, offset: 10851},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 395, col: 27, offset: 10856},
								name: "Literal",
							},
						},
						&labeledExpr{
							pos:   position{line: 395, col: 35, offset: 10864},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 395, col: 40, offset: 10869},
								expr: &seqExpr{
									pos: position{line: 395, col: 42, offset: 10871},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 395, col: 42, offset: 10871},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 395, col: 44, offset: 10873},
											val:        ";",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 395, col: 48, offset: 10877},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 395, col: 50, offset: 10879},
											name: "Literal",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Literal",
			pos:  position{line: 399, col: 1, offset: 10954},
			expr: &actionExpr{
				pos: position{line: 399, col: 12, offset: 10965},
				run: (*parser).callonLiteral1,
				expr: &seqExpr{
					pos: position{line: 399, col: 12, offset: 10965},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 399, col: 12, offset: 10965},
							label: "neg",
							expr: &zeroOrOneExpr{
								pos: position{line: 399, col: 16, offset: 10969},
								expr: &seqExpr{
									pos: position{line: 399, col: 18, offset: 10971},
									exprs: []interface{}{
										&litMatcher{
											pos:        position{line: 399, col: 18, offset: 10971},
											val:        "not",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 399, col: 24, offset: 10977},
											name: "ws",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 399, col: 30, offset: 10983},
							label: "val",
							expr: &ruleRefExpr{
								pos:  position{line: 399, col: 34, offset: 10987},
								name: "Expr",
							},
						},
						&labeledExpr{
							pos:   position{line: 399, col: 39, offset: 10992},
							label: "with",
							expr: &zeroOrOneExpr{
								pos: position{line: 399, col: 44, offset: 10997},
								expr: &seqExpr{
									pos: position{line: 399, col: 46, offset: 10999},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 399, col: 46, offset: 10999},
											name: "ws",
										},
										&ruleRefExpr{
											pos:  position{line: 399, col: 49, offset: 11002},
											name: "With",
										},
										&zeroOrMoreExpr{
											pos: position{line: 399, col: 54, offset: 11007},
											expr: &seqExpr{
												pos: position{line: 399, col: 55, offset: 11008},
												exprs: []interface{}{
													&ruleRefExpr{
														pos:  position{line: 399, col: 55, offset: 11008},
														name: "ws",
													},
													&ruleRefExpr{
														pos:  position{line: 399, col: 58, offset: 11011},
														name: "With",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "With",
			pos:  position{line: 427, col: 1, offset: 11682},
			expr: &actionExpr{
				pos: position{line: 427, col: 9, offset: 11690},
				run: (*parser).callonWith1,
				expr: &seqExpr{
					pos: position{line: 427, col: 9, offset: 11690},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 427, col: 9, offset: 11690},
							val:        "with",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 427, col: 16, offset: 11697},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 427, col: 19, offset: 11700},
							label: "target",
							expr: &ruleRefExpr{
								pos:  position{line: 427, col: 26, offset: 11707},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 427, col: 31, offset: 11712},
							name: "ws",
						},
						&litMatcher{
							pos:        position{line: 427, col: 34, offset: 11715},
							val:        "as",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 427, col: 39, offset: 11720},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 427, col: 42, offset: 11723},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 427, col: 48, offset: 11729},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "Expr",
			pos:  position{line: 438, col: 1, offset: 11978},
			expr: &choiceExpr{
				pos: position{line: 438, col: 9, offset: 11986},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 438, col: 9, offset: 11986},
						name: "InfixExpr",
					},
					&ruleRefExpr{
						pos:  position{line: 438, col: 21, offset: 11998},
						name: "PrefixExpr",
					},
					&ruleRefExpr{
						pos:  position{line: 438, col: 34, offset: 12011},
						name: "Term",
					},
				},
			},
		},
		{
			name: "InfixExpr",
			pos:  position{line: 440, col: 1, offset: 12017},
			expr: &choiceExpr{
				pos: position{line: 440, col: 14, offset: 12030},
				alternatives: []interface{}{
					&choiceExpr{
						pos: position{line: 440, col: 15, offset: 12031},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 440, col: 15, offset: 12031},
								name: "InfixCallExpr",
							},
							&ruleRefExpr{
								pos:  position{line: 440, col: 31, offset: 12047},
								name: "InfixCallExprReverse",
							},
						},
					},
					&choiceExpr{
						pos: position{line: 440, col: 56, offset: 12072},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 440, col: 56, offset: 12072},
								name: "InfixArithExpr",
							},
							&ruleRefExpr{
								pos:  position{line: 440, col: 73, offset: 12089},
								name: "InfixArithExprReverse",
							},
						},
					},
					&ruleRefExpr{
						pos:  position{line: 440, col: 98, offset: 12114},
						name: "InfixRelationExpr",
					},
				},
			},
		},
		{
			name: "InfixCallExpr",
			pos:  position{line: 442, col: 1, offset: 12133},
			expr: &actionExpr{
				pos: position{line: 442, col: 18, offset: 12150},
				run: (*parser).callonInfixCallExpr1,
				expr: &seqExpr{
					pos: position{line: 442, col: 18, offset: 12150},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 442, col: 18, offset: 12150},
							label: "output",
							expr: &ruleRefExpr{
								pos:  position{line: 442, col: 25, offset: 12157},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 442, col: 30, offset: 12162},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 442, col: 32, offset: 12164},
							val:        "=",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 442, col: 36, offset: 12168},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 442, col: 38, offset: 12170},
							label: "operator",
							expr: &ruleRefExpr{
								pos:  position{line: 442, col: 47, offset: 12179},
								name: "Operator",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 442, col: 56, offset: 12188},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 442, col: 58, offset: 12190},
							val:        "(",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 442, col: 62, offset: 12194},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 442, col: 64, offset: 12196},
							label: "args",
							expr: &ruleRefExpr{
								pos:  position{line: 442, col: 69, offset: 12201},
								name: "Args",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 442, col: 74, offset: 12206},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 442, col: 76, offset: 12208},
							val:        ")",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "InfixCallExprReverse",
			pos:  position{line: 446, col: 1, offset: 12270},
			expr: &actionExpr{
				pos: position{line: 446, col: 25, offset: 12294},
				run: (*parser).callonInfixCallExprReverse1,
				expr: &seqExpr{
					pos: position{line: 446, col: 25, offset: 12294},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 446, col: 25, offset: 12294},
							label: "operator",
							expr: &ruleRefExpr{
								pos:  position{line: 446, col: 34, offset: 12303},
								name: "Operator",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 446, col: 43, offset: 12312},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 446, col: 45, offset: 12314},
							val:        "(",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 446, col: 49, offset: 12318},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 446, col: 51, offset: 12320},
							label: "args",
							expr: &ruleRefExpr{
								pos:  position{line: 446, col: 56, offset: 12325},
								name: "Args",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 446, col: 61, offset: 12330},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 446, col: 63, offset: 12332},
							val:        ")",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 446, col: 67, offset: 12336},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 446, col: 69, offset: 12338},
							val:        "=",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 446, col: 73, offset: 12342},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 446, col: 75, offset: 12344},
							label: "output",
							expr: &ruleRefExpr{
								pos:  position{line: 446, col: 82, offset: 12351},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "InfixArithExpr",
			pos:  position{line: 450, col: 1, offset: 12414},
			expr: &actionExpr{
				pos: position{line: 450, col: 19, offset: 12432},
				run: (*parser).callonInfixArithExpr1,
				expr: &seqExpr{
					pos: position{line: 450, col: 19, offset: 12432},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 450, col: 19, offset: 12432},
							label: "output",
							expr: &ruleRefExpr{
								pos:  position{line: 450, col: 26, offset: 12439},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 450, col: 31, offset: 12444},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 450, col: 33, offset: 12446},
							val:        "=",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 450, col: 37, offset: 12450},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 450, col: 39, offset: 12452},
							label: "left",
							expr: &ruleRefExpr{
								pos:  position{line: 450, col: 44, offset: 12457},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 450, col: 49, offset: 12462},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 450, col: 51, offset: 12464},
							label: "operator",
							expr: &ruleRefExpr{
								pos:  position{line: 450, col: 60, offset: 12473},
								name: "ArithInfixOp",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 450, col: 73, offset: 12486},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 450, col: 75, offset: 12488},
							label: "right",
							expr: &ruleRefExpr{
								pos:  position{line: 450, col: 81, offset: 12494},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "InfixArithExprReverse",
			pos:  position{line: 454, col: 1, offset: 12586},
			expr: &actionExpr{
				pos: position{line: 454, col: 26, offset: 12611},
				run: (*parser).callonInfixArithExprReverse1,
				expr: &seqExpr{
					pos: position{line: 454, col: 26, offset: 12611},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 454, col: 26, offset: 12611},
							label: "left",
							expr: &ruleRefExpr{
								pos:  position{line: 454, col: 31, offset: 12616},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 454, col: 36, offset: 12621},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 454, col: 38, offset: 12623},
							label: "operator",
							expr: &ruleRefExpr{
								pos:  position{line: 454, col: 47, offset: 12632},
								name: "ArithInfixOp",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 454, col: 60, offset: 12645},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 454, col: 62, offset: 12647},
							label: "right",
							expr: &ruleRefExpr{
								pos:  position{line: 454, col: 68, offset: 12653},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 454, col: 73, offset: 12658},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 454, col: 75, offset: 12660},
							val:        "=",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 454, col: 79, offset: 12664},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 454, col: 81, offset: 12666},
							label: "output",
							expr: &ruleRefExpr{
								pos:  position{line: 454, col: 88, offset: 12673},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "ArithInfixOp",
			pos:  position{line: 458, col: 1, offset: 12765},
			expr: &actionExpr{
				pos: position{line: 458, col: 17, offset: 12781},
				run: (*parser).callonArithInfixOp1,
				expr: &labeledExpr{
					pos:   position{line: 458, col: 17, offset: 12781},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 458, col: 22, offset: 12786},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 458, col: 22, offset: 12786},
								val:        "+",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 458, col: 28, offset: 12792},
								val:        "-",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 458, col: 34, offset: 12798},
								val:        "*",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 458, col: 40, offset: 12804},
								val:        "/",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 458, col: 46, offset: 12810},
								val:        "&",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 458, col: 52, offset: 12816},
								val:        "|",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 458, col: 58, offset: 12822},
								val:        "-",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "InfixRelationExpr",
			pos:  position{line: 470, col: 1, offset: 13096},
			expr: &actionExpr{
				pos: position{line: 470, col: 22, offset: 13117},
				run: (*parser).callonInfixRelationExpr1,
				expr: &seqExpr{
					pos: position{line: 470, col: 22, offset: 13117},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 470, col: 22, offset: 13117},
							label: "left",
							expr: &ruleRefExpr{
								pos:  position{line: 470, col: 27, offset: 13122},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 470, col: 32, offset: 13127},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 470, col: 34, offset: 13129},
							label: "operator",
							expr: &ruleRefExpr{
								pos:  position{line: 470, col: 43, offset: 13138},
								name: "InfixRelationOp",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 470, col: 59, offset: 13154},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 470, col: 61, offset: 13156},
							label: "right",
							expr: &ruleRefExpr{
								pos:  position{line: 470, col: 67, offset: 13162},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "InfixRelationOp",
			pos:  position{line: 481, col: 1, offset: 13340},
			expr: &actionExpr{
				pos: position{line: 481, col: 20, offset: 13359},
				run: (*parser).callonInfixRelationOp1,
				expr: &labeledExpr{
					pos:   position{line: 481, col: 20, offset: 13359},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 481, col: 25, offset: 13364},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 481, col: 25, offset: 13364},
								val:        "=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 481, col: 31, offset: 13370},
								val:        "!=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 481, col: 38, offset: 13377},
								val:        "<=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 481, col: 45, offset: 13384},
								val:        ">=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 481, col: 52, offset: 13391},
								val:        "<",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 481, col: 58, offset: 13397},
								val:        ">",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "PrefixExpr",
			pos:  position{line: 493, col: 1, offset: 13671},
			expr: &choiceExpr{
				pos: position{line: 493, col: 15, offset: 13685},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 493, col: 15, offset: 13685},
						name: "SetEmpty",
					},
					&ruleRefExpr{
						pos:  position{line: 493, col: 26, offset: 13696},
						name: "Call",
					},
				},
			},
		},
		{
			name: "Call",
			pos:  position{line: 495, col: 1, offset: 13702},
			expr: &actionExpr{
				pos: position{line: 495, col: 9, offset: 13710},
				run: (*parser).callonCall1,
				expr: &seqExpr{
					pos: position{line: 495, col: 9, offset: 13710},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 495, col: 9, offset: 13710},
							label: "name",
							expr: &ruleRefExpr{
								pos:  position{line: 495, col: 14, offset: 13715},
								name: "Operator",
							},
						},
						&litMatcher{
							pos:        position{line: 495, col: 23, offset: 13724},
							val:        "(",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 495, col: 27, offset: 13728},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 495, col: 29, offset: 13730},
							label: "head",
							expr: &zeroOrOneExpr{
								pos: position{line: 495, col: 34, offset: 13735},
								expr: &ruleRefExpr{
									pos:  position{line: 495, col: 34, offset: 13735},
									name: "Term",
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 495, col: 40, offset: 13741},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 495, col: 45, offset: 13746},
								expr: &seqExpr{
									pos: position{line: 495, col: 47, offset: 13748},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 495, col: 47, offset: 13748},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 495, col: 49, offset: 13750},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 495, col: 53, offset: 13754},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 495, col: 55, offset: 13756},
											name: "Term",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 495, col: 63, offset: 13764},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 495, col: 66, offset: 13767},
							val:        ")",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Operator",
			pos:  position{line: 513, col: 1, offset: 14201},
			expr: &actionExpr{
				pos: position{line: 513, col: 13, offset: 14213},
				run: (*parser).callonOperator1,
				expr: &labeledExpr{
					pos:   position{line: 513, col: 13, offset: 14213},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 513, col: 18, offset: 14218},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 513, col: 18, offset: 14218},
								name: "Ref",
							},
							&ruleRefExpr{
								pos:  position{line: 513, col: 24, offset: 14224},
								name: "Var",
							},
						},
					},
				},
			},
		},
		{
			name: "Term",
			pos:  position{line: 525, col: 1, offset: 14455},
			expr: &actionExpr{
				pos: position{line: 525, col: 9, offset: 14463},
				run: (*parser).callonTerm1,
				expr: &labeledExpr{
					pos:   position{line: 525, col: 9, offset: 14463},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 525, col: 15, offset: 14469},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 525, col: 15, offset: 14469},
								name: "Comprehension",
							},
							&ruleRefExpr{
								pos:  position{line: 525, col: 31, offset: 14485},
								name: "Composite",
							},
							&ruleRefExpr{
								pos:  position{line: 525, col: 43, offset: 14497},
								name: "Scalar",
							},
							&ruleRefExpr{
								pos:  position{line: 525, col: 52, offset: 14506},
								name: "Ref",
							},
							&ruleRefExpr{
								pos:  position{line: 525, col: 58, offset: 14512},
								name: "Var",
							},
						},
					},
				},
			},
		},
		{
			name: "Comprehension",
			pos:  position{line: 529, col: 1, offset: 14543},
			expr: &choiceExpr{
				pos: position{line: 529, col: 18, offset: 14560},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 529, col: 18, offset: 14560},
						name: "ArrayComprehension",
					},
					&ruleRefExpr{
						pos:  position{line: 529, col: 39, offset: 14581},
						name: "ObjectComprehension",
					},
					&ruleRefExpr{
						pos:  position{line: 529, col: 61, offset: 14603},
						name: "SetComprehension",
					},
				},
			},
		},
		{
			name: "ArrayComprehension",
			pos:  position{line: 531, col: 1, offset: 14621},
			expr: &actionExpr{
				pos: position{line: 531, col: 23, offset: 14643},
				run: (*parser).callonArrayComprehension1,
				expr: &seqExpr{
					pos: position{line: 531, col: 23, offset: 14643},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 531, col: 23, offset: 14643},
							val:        "[",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 531, col: 27, offset: 14647},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 531, col: 29, offset: 14649},
							label: "term",
							expr: &ruleRefExpr{
								pos:  position{line: 531, col: 34, offset: 14654},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 531, col: 39, offset: 14659},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 531, col: 41, offset: 14661},
							val:        "|",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 531, col: 45, offset: 14665},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 531, col: 47, offset: 14667},
							label: "body",
							expr: &ruleRefExpr{
								pos:  position{line: 531, col: 52, offset: 14672},
								name: "WhitespaceBody",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 531, col: 67, offset: 14687},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 531, col: 69, offset: 14689},
							val:        "]",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "ObjectComprehension",
			pos:  position{line: 537, col: 1, offset: 14814},
			expr: &actionExpr{
				pos: position{line: 537, col: 24, offset: 14837},
				run: (*parser).callonObjectComprehension1,
				expr: &seqExpr{
					pos: position{line: 537, col: 24, offset: 14837},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 537, col: 24, offset: 14837},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 537, col: 28, offset: 14841},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 537, col: 30, offset: 14843},
							label: "key",
							expr: &ruleRefExpr{
								pos:  position{line: 537, col: 34, offset: 14847},
								name: "Key",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 537, col: 38, offset: 14851},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 537, col: 40, offset: 14853},
							val:        ":",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 537, col: 44, offset: 14857},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 537, col: 46, offset: 14859},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 537, col: 52, offset: 14865},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 537, col: 58, offset: 14871},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 537, col: 60, offset: 14873},
							val:        "|",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 537, col: 64, offset: 14877},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 537, col: 66, offset: 14879},
							label: "body",
							expr: &ruleRefExpr{
								pos:  position{line: 537, col: 71, offset: 14884},
								name: "WhitespaceBody",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 537, col: 86, offset: 14899},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 537, col: 88, offset: 14901},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "SetComprehension",
			pos:  position{line: 543, col: 1, offset: 15041},
			expr: &actionExpr{
				pos: position{line: 543, col: 21, offset: 15061},
				run: (*parser).callonSetComprehension1,
				expr: &seqExpr{
					pos: position{line: 543, col: 21, offset: 15061},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 543, col: 21, offset: 15061},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 543, col: 25, offset: 15065},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 543, col: 27, offset: 15067},
							label: "term",
							expr: &ruleRefExpr{
								pos:  position{line: 543, col: 32, offset: 15072},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 543, col: 37, offset: 15077},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 543, col: 39, offset: 15079},
							val:        "|",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 543, col: 43, offset: 15083},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 543, col: 45, offset: 15085},
							label: "body",
							expr: &ruleRefExpr{
								pos:  position{line: 543, col: 50, offset: 15090},
								name: "WhitespaceBody",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 543, col: 65, offset: 15105},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 543, col: 67, offset: 15107},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Composite",
			pos:  position{line: 549, col: 1, offset: 15230},
			expr: &choiceExpr{
				pos: position{line: 549, col: 14, offset: 15243},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 549, col: 14, offset: 15243},
						name: "Object",
					},
					&ruleRefExpr{
						pos:  position{line: 549, col: 23, offset: 15252},
						name: "Array",
					},
					&ruleRefExpr{
						pos:  position{line: 549, col: 31, offset: 15260},
						name: "Set",
					},
				},
			},
		},
		{
			name: "Scalar",
			pos:  position{line: 551, col: 1, offset: 15265},
			expr: &choiceExpr{
				pos: position{line: 551, col: 11, offset: 15275},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 551, col: 11, offset: 15275},
						name: "Number",
					},
					&ruleRefExpr{
						pos:  position{line: 551, col: 20, offset: 15284},
						name: "String",
					},
					&ruleRefExpr{
						pos:  position{line: 551, col: 29, offset: 15293},
						name: "Bool",
					},
					&ruleRefExpr{
						pos:  position{line: 551, col: 36, offset: 15300},
						name: "Null",
					},
				},
			},
		},
		{
			name: "Key",
			pos:  position{line: 553, col: 1, offset: 15306},
			expr: &choiceExpr{
				pos: position{line: 553, col: 8, offset: 15313},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 553, col: 8, offset: 15313},
						name: "Scalar",
					},
					&ruleRefExpr{
						pos:  position{line: 553, col: 17, offset: 15322},
						name: "Ref",
					},
					&ruleRefExpr{
						pos:  position{line: 553, col: 23, offset: 15328},
						name: "Var",
					},
				},
			},
		},
		{
			name: "Object",
			pos:  position{line: 555, col: 1, offset: 15333},
			expr: &actionExpr{
				pos: position{line: 555, col: 11, offset: 15343},
				run: (*parser).callonObject1,
				expr: &seqExpr{
					pos: position{line: 555, col: 11, offset: 15343},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 555, col: 11, offset: 15343},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 555, col: 15, offset: 15347},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 555, col: 17, offset: 15349},
							label: "head",
							expr: &zeroOrOneExpr{
								pos: position{line: 555, col: 22, offset: 15354},
								expr: &seqExpr{
									pos: position{line: 555, col: 23, offset: 15355},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 555, col: 23, offset: 15355},
											name: "Key",
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 27, offset: 15359},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 555, col: 29, offset: 15361},
											val:        ":",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 33, offset: 15365},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 35, offset: 15367},
											name: "Term",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 555, col: 42, offset: 15374},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 555, col: 47, offset: 15379},
								expr: &seqExpr{
									pos: position{line: 555, col: 49, offset: 15381},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 555, col: 49, offset: 15381},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 555, col: 51, offset: 15383},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 55, offset: 15387},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 57, offset: 15389},
											name: "Key",
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 61, offset: 15393},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 555, col: 63, offset: 15395},
											val:        ":",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 67, offset: 15399},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 555, col: 69, offset: 15401},
											name: "Term",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 555, col: 77, offset: 15409},
							name: "_",
						},
						&zeroOrOneExpr{
							pos: position{line: 555, col: 79, offset: 15411},
							expr: &litMatcher{
								pos:        position{line: 555, col: 79, offset: 15411},
								val:        ",",
								ignoreCase: false,
							},
						},
						&ruleRefExpr{
							pos:  position{line: 555, col: 84, offset: 15416},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 555, col: 86, offset: 15418},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Array",
			pos:  position{line: 559, col: 1, offset: 15481},
			expr: &actionExpr{
				pos: position{line: 559, col: 10, offset: 15490},
				run: (*parser).callonArray1,
				expr: &seqExpr{
					pos: position{line: 559, col: 10, offset: 15490},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 559, col: 10, offset: 15490},
							val:        "[",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 559, col: 14, offset: 15494},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 559, col: 17, offset: 15497},
							label: "head",
							expr: &zeroOrOneExpr{
								pos: position{line: 559, col: 22, offset: 15502},
								expr: &ruleRefExpr{
									pos:  position{line: 559, col: 22, offset: 15502},
									name: "Term",
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 559, col: 28, offset: 15508},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 559, col: 33, offset: 15513},
								expr: &seqExpr{
									pos: position{line: 559, col: 34, offset: 15514},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 559, col: 34, offset: 15514},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 559, col: 36, offset: 15516},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 559, col: 40, offset: 15520},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 559, col: 42, offset: 15522},
											name: "Term",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 559, col: 49, offset: 15529},
							name: "_",
						},
						&zeroOrOneExpr{
							pos: position{line: 559, col: 51, offset: 15531},
							expr: &litMatcher{
								pos:        position{line: 559, col: 51, offset: 15531},
								val:        ",",
								ignoreCase: false,
							},
						},
						&ruleRefExpr{
							pos:  position{line: 559, col: 56, offset: 15536},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 559, col: 59, offset: 15539},
							val:        "]",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Set",
			pos:  position{line: 563, col: 1, offset: 15601},
			expr: &choiceExpr{
				pos: position{line: 563, col: 8, offset: 15608},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 563, col: 8, offset: 15608},
						name: "SetEmpty",
					},
					&ruleRefExpr{
						pos:  position{line: 563, col: 19, offset: 15619},
						name: "SetNonEmpty",
					},
				},
			},
		},
		{
			name: "SetEmpty",
			pos:  position{line: 565, col: 1, offset: 15632},
			expr: &actionExpr{
				pos: position{line: 565, col: 13, offset: 15644},
				run: (*parser).callonSetEmpty1,
				expr: &seqExpr{
					pos: position{line: 565, col: 13, offset: 15644},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 565, col: 13, offset: 15644},
							val:        "set(",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 565, col: 20, offset: 15651},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 565, col: 22, offset: 15653},
							val:        ")",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "SetNonEmpty",
			pos:  position{line: 571, col: 1, offset: 15741},
			expr: &actionExpr{
				pos: position{line: 571, col: 16, offset: 15756},
				run: (*parser).callonSetNonEmpty1,
				expr: &seqExpr{
					pos: position{line: 571, col: 16, offset: 15756},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 571, col: 16, offset: 15756},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 571, col: 20, offset: 15760},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 571, col: 22, offset: 15762},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 571, col: 27, offset: 15767},
								name: "Term",
							},
						},
						&labeledExpr{
							pos:   position{line: 571, col: 32, offset: 15772},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 571, col: 37, offset: 15777},
								expr: &seqExpr{
									pos: position{line: 571, col: 38, offset: 15778},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 571, col: 38, offset: 15778},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 571, col: 40, offset: 15780},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 571, col: 44, offset: 15784},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 571, col: 46, offset: 15786},
											name: "Term",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 571, col: 53, offset: 15793},
							name: "_",
						},
						&zeroOrOneExpr{
							pos: position{line: 571, col: 55, offset: 15795},
							expr: &litMatcher{
								pos:        position{line: 571, col: 55, offset: 15795},
								val:        ",",
								ignoreCase: false,
							},
						},
						&ruleRefExpr{
							pos:  position{line: 571, col: 60, offset: 15800},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 571, col: 62, offset: 15802},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Ref",
			pos:  position{line: 588, col: 1, offset: 16207},
			expr: &actionExpr{
				pos: position{line: 588, col: 8, offset: 16214},
				run: (*parser).callonRef1,
				expr: &seqExpr{
					pos: position{line: 588, col: 8, offset: 16214},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 588, col: 8, offset: 16214},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 588, col: 13, offset: 16219},
								name: "Var",
							},
						},
						&labeledExpr{
							pos:   position{line: 588, col: 17, offset: 16223},
							label: "tail",
							expr: &oneOrMoreExpr{
								pos: position{line: 588, col: 22, offset: 16228},
								expr: &choiceExpr{
									pos: position{line: 588, col: 24, offset: 16230},
									alternatives: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 588, col: 24, offset: 16230},
											name: "RefDot",
										},
										&ruleRefExpr{
											pos:  position{line: 588, col: 33, offset: 16239},
											name: "RefBracket",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "RefDot",
			pos:  position{line: 601, col: 1, offset: 16478},
			expr: &actionExpr{
				pos: position{line: 601, col: 11, offset: 16488},
				run: (*parser).callonRefDot1,
				expr: &seqExpr{
					pos: position{line: 601, col: 11, offset: 16488},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 601, col: 11, offset: 16488},
							val:        ".",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 601, col: 15, offset: 16492},
							label: "val",
							expr: &ruleRefExpr{
								pos:  position{line: 601, col: 19, offset: 16496},
								name: "Var",
							},
						},
					},
				},
			},
		},
		{
			name: "RefBracket",
			pos:  position{line: 608, col: 1, offset: 16715},
			expr: &actionExpr{
				pos: position{line: 608, col: 15, offset: 16729},
				run: (*parser).callonRefBracket1,
				expr: &seqExpr{
					pos: position{line: 608, col: 15, offset: 16729},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 608, col: 15, offset: 16729},
							val:        "[",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 608, col: 19, offset: 16733},
							label: "val",
							expr: &choiceExpr{
								pos: position{line: 608, col: 24, offset: 16738},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 608, col: 24, offset: 16738},
										name: "Composite",
									},
									&ruleRefExpr{
										pos:  position{line: 608, col: 36, offset: 16750},
										name: "Ref",
									},
									&ruleRefExpr{
										pos:  position{line: 608, col: 42, offset: 16756},
										name: "Scalar",
									},
									&ruleRefExpr{
										pos:  position{line: 608, col: 51, offset: 16765},
										name: "Var",
									},
								},
							},
						},
						&litMatcher{
							pos:        position{line: 608, col: 56, offset: 16770},
							val:        "]",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Var",
			pos:  position{line: 612, col: 1, offset: 16799},
			expr: &actionExpr{
				pos: position{line: 612, col: 8, offset: 16806},
				run: (*parser).callonVar1,
				expr: &labeledExpr{
					pos:   position{line: 612, col: 8, offset: 16806},
					label: "val",
					expr: &ruleRefExpr{
						pos:  position{line: 612, col: 12, offset: 16810},
						name: "VarChecked",
					},
				},
			},
		},
		{
			name: "VarChecked",
			pos:  position{line: 617, col: 1, offset: 16932},
			expr: &seqExpr{
				pos: position{line: 617, col: 15, offset: 16946},
				exprs: []interface{}{
					&labeledExpr{
						pos:   position{line: 617, col: 15, offset: 16946},
						label: "val",
						expr: &ruleRefExpr{
							pos:  position{line: 617, col: 19, offset: 16950},
							name: "VarUnchecked",
						},
					},
					&notCodeExpr{
						pos: position{line: 617, col: 32, offset: 16963},
						run: (*parser).callonVarChecked4,
					},
				},
			},
		},
		{
			name: "VarUnchecked",
			pos:  position{line: 621, col: 1, offset: 17028},
			expr: &actionExpr{
				pos: position{line: 621, col: 17, offset: 17044},
				run: (*parser).callonVarUnchecked1,
				expr: &seqExpr{
					pos: position{line: 621, col: 17, offset: 17044},
					exprs: []interface{}{
						&ruleRefExpr{
							pos:  position{line: 621, col: 17, offset: 17044},
							name: "AsciiLetter",
						},
						&zeroOrMoreExpr{
							pos: position{line: 621, col: 29, offset: 17056},
							expr: &choiceExpr{
								pos: position{line: 621, col: 30, offset: 17057},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 621, col: 30, offset: 17057},
										name: "AsciiLetter",
									},
									&ruleRefExpr{
										pos:  position{line: 621, col: 44, offset: 17071},
										name: "DecimalDigit",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Number",
			pos:  position{line: 628, col: 1, offset: 17214},
			expr: &actionExpr{
				pos: position{line: 628, col: 11, offset: 17224},
				run: (*parser).callonNumber1,
				expr: &seqExpr{
					pos: position{line: 628, col: 11, offset: 17224},
					exprs: []interface{}{
						&zeroOrOneExpr{
							pos: position{line: 628, col: 11, offset: 17224},
							expr: &litMatcher{
								pos:        position{line: 628, col: 11, offset: 17224},
								val:        "-",
								ignoreCase: false,
							},
						},
						&choiceExpr{
							pos: position{line: 628, col: 18, offset: 17231},
							alternatives: []interface{}{
								&ruleRefExpr{
									pos:  position{line: 628, col: 18, offset: 17231},
									name: "Float",
								},
								&ruleRefExpr{
									pos:  position{line: 628, col: 26, offset: 17239},
									name: "Integer",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Float",
			pos:  position{line: 641, col: 1, offset: 17630},
			expr: &choiceExpr{
				pos: position{line: 641, col: 10, offset: 17639},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 641, col: 10, offset: 17639},
						name: "ExponentFloat",
					},
					&ruleRefExpr{
						pos:  position{line: 641, col: 26, offset: 17655},
						name: "PointFloat",
					},
				},
			},
		},
		{
			name: "ExponentFloat",
			pos:  position{line: 643, col: 1, offset: 17667},
			expr: &seqExpr{
				pos: position{line: 643, col: 18, offset: 17684},
				exprs: []interface{}{
					&choiceExpr{
						pos: position{line: 643, col: 20, offset: 17686},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 643, col: 20, offset: 17686},
								name: "PointFloat",
							},
							&ruleRefExpr{
								pos:  position{line: 643, col: 33, offset: 17699},
								name: "Integer",
							},
						},
					},
					&ruleRefExpr{
						pos:  position{line: 643, col: 43, offset: 17709},
						name: "Exponent",
					},
				},
			},
		},
		{
			name: "PointFloat",
			pos:  position{line: 645, col: 1, offset: 17719},
			expr: &seqExpr{
				pos: position{line: 645, col: 15, offset: 17733},
				exprs: []interface{}{
					&zeroOrOneExpr{
						pos: position{line: 645, col: 15, offset: 17733},
						expr: &ruleRefExpr{
							pos:  position{line: 645, col: 15, offset: 17733},
							name: "Integer",
						},
					},
					&ruleRefExpr{
						pos:  position{line: 645, col: 24, offset: 17742},
						name: "Fraction",
					},
				},
			},
		},
		{
			name: "Fraction",
			pos:  position{line: 647, col: 1, offset: 17752},
			expr: &seqExpr{
				pos: position{line: 647, col: 13, offset: 17764},
				exprs: []interface{}{
					&litMatcher{
						pos:        position{line: 647, col: 13, offset: 17764},
						val:        ".",
						ignoreCase: false,
					},
					&oneOrMoreExpr{
						pos: position{line: 647, col: 17, offset: 17768},
						expr: &ruleRefExpr{
							pos:  position{line: 647, col: 17, offset: 17768},
							name: "DecimalDigit",
						},
					},
				},
			},
		},
		{
			name: "Exponent",
			pos:  position{line: 649, col: 1, offset: 17783},
			expr: &seqExpr{
				pos: position{line: 649, col: 13, offset: 17795},
				exprs: []interface{}{
					&litMatcher{
						pos:        position{line: 649, col: 13, offset: 17795},
						val:        "e",
						ignoreCase: true,
					},
					&zeroOrOneExpr{
						pos: position{line: 649, col: 18, offset: 17800},
						expr: &charClassMatcher{
							pos:        position{line: 649, col: 18, offset: 17800},
							val:        "[+-]",
							chars:      []rune{'+', '-'},
							ignoreCase: false,
							inverted:   false,
						},
					},
					&oneOrMoreExpr{
						pos: position{line: 649, col: 24, offset: 17806},
						expr: &ruleRefExpr{
							pos:  position{line: 649, col: 24, offset: 17806},
							name: "DecimalDigit",
						},
					},
				},
			},
		},
		{
			name: "Integer",
			pos:  position{line: 651, col: 1, offset: 17821},
			expr: &choiceExpr{
				pos: position{line: 651, col: 12, offset: 17832},
				alternatives: []interface{}{
					&litMatcher{
						pos:        position{line: 651, col: 12, offset: 17832},
						val:        "0",
						ignoreCase: false,
					},
					&seqExpr{
						pos: position{line: 651, col: 20, offset: 17840},
						exprs: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 651, col: 20, offset: 17840},
								name: "NonZeroDecimalDigit",
							},
							&zeroOrMoreExpr{
								pos: position{line: 651, col: 40, offset: 17860},
								expr: &ruleRefExpr{
									pos:  position{line: 651, col: 40, offset: 17860},
									name: "DecimalDigit",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "String",
			pos:  position{line: 653, col: 1, offset: 17877},
			expr: &choiceExpr{
				pos: position{line: 653, col: 11, offset: 17887},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 653, col: 11, offset: 17887},
						name: "QuotedString",
					},
					&ruleRefExpr{
						pos:  position{line: 653, col: 26, offset: 17902},
						name: "RawString",
					},
				},
			},
		},
		{
			name: "QuotedString",
			pos:  position{line: 655, col: 1, offset: 17913},
			expr: &actionExpr{
				pos: position{line: 655, col: 17, offset: 17929},
				run: (*parser).callonQuotedString1,
				expr: &seqExpr{
					pos: position{line: 655, col: 17, offset: 17929},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 655, col: 17, offset: 17929},
							val:        "\"",
							ignoreCase: false,
						},
						&zeroOrMoreExpr{
							pos: position{line: 655, col: 21, offset: 17933},
							expr: &ruleRefExpr{
								pos:  position{line: 655, col: 21, offset: 17933},
								name: "Char",
							},
						},
						&litMatcher{
							pos:        position{line: 655, col: 27, offset: 17939},
							val:        "\"",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "RawString",
			pos:  position{line: 663, col: 1, offset: 18094},
			expr: &actionExpr{
				pos: position{line: 663, col: 14, offset: 18107},
				run: (*parser).callonRawString1,
				expr: &seqExpr{
					pos: position{line: 663, col: 14, offset: 18107},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 663, col: 14, offset: 18107},
							val:        "`",
							ignoreCase: false,
						},
						&zeroOrMoreExpr{
							pos: position{line: 663, col: 18, offset: 18111},
							expr: &charClassMatcher{
								pos:        position{line: 663, col: 18, offset: 18111},
								val:        "[^`]",
								chars:      []rune{'`'},
								ignoreCase: false,
								inverted:   true,
							},
						},
						&litMatcher{
							pos:        position{line: 663, col: 24, offset: 18117},
							val:        "`",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Bool",
			pos:  position{line: 672, col: 1, offset: 18284},
			expr: &choiceExpr{
				pos: position{line: 672, col: 9, offset: 18292},
				alternatives: []interface{}{
					&actionExpr{
						pos: position{line: 672, col: 9, offset: 18292},
						run: (*parser).callonBool2,
						expr: &litMatcher{
							pos:        position{line: 672, col: 9, offset: 18292},
							val:        "true",
							ignoreCase: false,
						},
					},
					&actionExpr{
						pos: position{line: 676, col: 5, offset: 18392},
						run: (*parser).callonBool4,
						expr: &litMatcher{
							pos:        position{line: 676, col: 5, offset: 18392},
							val:        "false",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Null",
			pos:  position{line: 682, col: 1, offset: 18493},
			expr: &actionExpr{
				pos: position{line: 682, col: 9, offset: 18501},
				run: (*parser).callonNull1,
				expr: &litMatcher{
					pos:        position{line: 682, col: 9, offset: 18501},
					val:        "null",
					ignoreCase: false,
				},
			},
		},
		{
			name: "AsciiLetter",
			pos:  position{line: 688, col: 1, offset: 18596},
			expr: &charClassMatcher{
				pos:        position{line: 688, col: 16, offset: 18611},
				val:        "[A-Za-z_]",
				chars:      []rune{'_'},
				ranges:     []rune{'A', 'Z', 'a', 'z'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "Char",
			pos:  position{line: 690, col: 1, offset: 18622},
			expr: &choiceExpr{
				pos: position{line: 690, col: 9, offset: 18630},
				alternatives: []interface{}{
					&seqExpr{
						pos: position{line: 690, col: 11, offset: 18632},
						exprs: []interface{}{
							&notExpr{
								pos: position{line: 690, col: 11, offset: 18632},
								expr: &ruleRefExpr{
									pos:  position{line: 690, col: 12, offset: 18633},
									name: "EscapedChar",
								},
							},
							&anyMatcher{
								line: 690, col: 24, offset: 18645,
							},
						},
					},
					&seqExpr{
						pos: position{line: 690, col: 32, offset: 18653},
						exprs: []interface{}{
							&litMatcher{
								pos:        position{line: 690, col: 32, offset: 18653},
								val:        "\\",
								ignoreCase: false,
							},
							&ruleRefExpr{
								pos:  position{line: 690, col: 37, offset: 18658},
								name: "EscapeSequence",
							},
						},
					},
				},
			},
		},
		{
			name: "EscapedChar",
			pos:  position{line: 692, col: 1, offset: 18676},
			expr: &charClassMatcher{
				pos:        position{line: 692, col: 16, offset: 18691},
				val:        "[\\x00-\\x1f\"\\\\]",
				chars:      []rune{'"', '\\'},
				ranges:     []rune{'\x00', '\x1f'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "EscapeSequence",
			pos:  position{line: 694, col: 1, offset: 18707},
			expr: &choiceExpr{
				pos: position{line: 694, col: 19, offset: 18725},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 694, col: 19, offset: 18725},
						name: "SingleCharEscape",
					},
					&ruleRefExpr{
						pos:  position{line: 694, col: 38, offset: 18744},
						name: "UnicodeEscape",
					},
				},
			},
		},
		{
			name: "SingleCharEscape",
			pos:  position{line: 696, col: 1, offset: 18759},
			expr: &charClassMatcher{
				pos:        position{line: 696, col: 21, offset: 18779},
				val:        "[ \" \\\\ / b f n r t ]",
				chars:      []rune{' ', '"', ' ', '\\', ' ', '/', ' ', 'b', ' ', 'f', ' ', 'n', ' ', 'r', ' ', 't', ' '},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "UnicodeEscape",
			pos:  position{line: 698, col: 1, offset: 18801},
			expr: &seqExpr{
				pos: position{line: 698, col: 18, offset: 18818},
				exprs: []interface{}{
					&litMatcher{
						pos:        position{line: 698, col: 18, offset: 18818},
						val:        "u",
						ignoreCase: false,
					},
					&ruleRefExpr{
						pos:  position{line: 698, col: 22, offset: 18822},
						name: "HexDigit",
					},
					&ruleRefExpr{
						pos:  position{line: 698, col: 31, offset: 18831},
						name: "HexDigit",
					},
					&ruleRefExpr{
						pos:  position{line: 698, col: 40, offset: 18840},
						name: "HexDigit",
					},
					&ruleRefExpr{
						pos:  position{line: 698, col: 49, offset: 18849},
						name: "HexDigit",
					},
				},
			},
		},
		{
			name: "DecimalDigit",
			pos:  position{line: 700, col: 1, offset: 18859},
			expr: &charClassMatcher{
				pos:        position{line: 700, col: 17, offset: 18875},
				val:        "[0-9]",
				ranges:     []rune{'0', '9'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "NonZeroDecimalDigit",
			pos:  position{line: 702, col: 1, offset: 18882},
			expr: &charClassMatcher{
				pos:        position{line: 702, col: 24, offset: 18905},
				val:        "[1-9]",
				ranges:     []rune{'1', '9'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "HexDigit",
			pos:  position{line: 704, col: 1, offset: 18912},
			expr: &charClassMatcher{
				pos:        position{line: 704, col: 13, offset: 18924},
				val:        "[0-9a-fA-F]",
				ranges:     []rune{'0', '9', 'a', 'f', 'A', 'F'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name:        "ws",
			displayName: "\"whitespace\"",
			pos:         position{line: 706, col: 1, offset: 18937},
			expr: &oneOrMoreExpr{
				pos: position{line: 706, col: 20, offset: 18956},
				expr: &charClassMatcher{
					pos:        position{line: 706, col: 20, offset: 18956},
					val:        "[ \\t\\r\\n]",
					chars:      []rune{' ', '\t', '\r', '\n'},
					ignoreCase: false,
					inverted:   false,
				},
			},
		},
		{
			name:        "_",
			displayName: "\"whitespace\"",
			pos:         position{line: 708, col: 1, offset: 18968},
			expr: &zeroOrMoreExpr{
				pos: position{line: 708, col: 19, offset: 18986},
				expr: &choiceExpr{
					pos: position{line: 708, col: 21, offset: 18988},
					alternatives: []interface{}{
						&charClassMatcher{
							pos:        position{line: 708, col: 21, offset: 18988},
							val:        "[ \\t\\r\\n]",
							chars:      []rune{' ', '\t', '\r', '\n'},
							ignoreCase: false,
							inverted:   false,
						},
						&ruleRefExpr{
							pos:  position{line: 708, col: 33, offset: 19000},
							name: "Comment",
						},
					},
				},
			},
		},
		{
			name: "Comment",
			pos:  position{line: 710, col: 1, offset: 19012},
			expr: &actionExpr{
				pos: position{line: 710, col: 12, offset: 19023},
				run: (*parser).callonComment1,
				expr: &seqExpr{
					pos: position{line: 710, col: 12, offset: 19023},
					exprs: []interface{}{
						&zeroOrMoreExpr{
							pos: position{line: 710, col: 12, offset: 19023},
							expr: &charClassMatcher{
								pos:        position{line: 710, col: 12, offset: 19023},
								val:        "[ \\t]",
								chars:      []rune{' ', '\t'},
								ignoreCase: false,
								inverted:   false,
							},
						},
						&litMatcher{
							pos:        position{line: 710, col: 19, offset: 19030},
							val:        "#",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 710, col: 23, offset: 19034},
							label: "text",
							expr: &zeroOrMoreExpr{
								pos: position{line: 710, col: 28, offset: 19039},
								expr: &charClassMatcher{
									pos:        position{line: 710, col: 28, offset: 19039},
									val:        "[^\\r\\n]",
									chars:      []rune{'\r', '\n'},
									ignoreCase: false,
									inverted:   true,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "EOF",
			pos:  position{line: 721, col: 1, offset: 19315},
			expr: &notExpr{
				pos: position{line: 721, col: 8, offset: 19322},
				expr: &anyMatcher{
					line: 721, col: 9, offset: 19323,
				},
			},
		},
	},
}

func (c *current) onProgram1(vals interface{}) (interface{}, error) {
	var buf []interface{}

	if vals == nil {
		return buf, nil
	}

	ifaceSlice := vals.([]interface{})
	head := ifaceSlice[0]
	buf = append(buf, head)
	for _, tail := range ifaceSlice[1].([]interface{}) {
		stmt := tail.([]interface{})[1]
		buf = append(buf, stmt)
	}

	return program{buf, c.globalStore[commentsKey]}, nil
}

func (p *parser) callonProgram1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onProgram1(stack["vals"])
}

func (c *current) onStmt1(val interface{}) (interface{}, error) {
	return val, nil
}

func (p *parser) callonStmt1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onStmt1(stack["val"])
}

func (c *current) onPackage1(val interface{}) (interface{}, error) {
	// All packages are implicitly declared under the default root document.
	term := val.(*Term)
	path := Ref{DefaultRootDocument.Copy().SetLocation(term.Location)}
	switch v := term.Value.(type) {
	case Ref:
		// Convert head of package Ref to String because it will be prefixed
		// with the root document variable.
		head := StringTerm(string(v[0].Value.(Var))).SetLocation(v[0].Location)
		tail := v[1:]
		if !tail.IsGround() {
			return nil, fmt.Errorf("package name cannot contain variables: %v", v)
		}

		// We do not allow non-string values in package names.
		// Because documents are typically represented as JSON, non-string keys are
		// not allowed for now.
		// TODO(tsandall): consider special syntax for namespacing under arrays.
		for _, p := range tail {
			_, ok := p.Value.(String)
			if !ok {
				return nil, fmt.Errorf("package name cannot contain non-string values: %v", v)
			}
		}
		path = append(path, head)
		path = append(path, tail...)
	case Var:
		s := StringTerm(string(v)).SetLocation(term.Location)
		path = append(path, s)
	}
	pkg := &Package{Location: currentLocation(c), Path: path}
	return pkg, nil
}

func (p *parser) callonPackage1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onPackage1(stack["val"])
}

func (c *current) onImport1(path, alias interface{}) (interface{}, error) {
	imp := &Import{}
	imp.Location = currentLocation(c)
	imp.Path = path.(*Term)
	if err := IsValidImportPath(imp.Path.Value); err != nil {
		return nil, err
	}
	if alias == nil {
		return imp, nil
	}
	aliasSlice := alias.([]interface{})
	// Import definition above describes the "alias" slice. We only care about the "Var" element.
	imp.Alias = aliasSlice[3].(*Term).Value.(Var)
	return imp, nil
}

func (p *parser) callonImport1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onImport1(stack["path"], stack["alias"])
}

func (c *current) onDefaultRules1(name, value interface{}) (interface{}, error) {

	term := value.(*Term)
	var err error

	vis := NewGenericVisitor(func(x interface{}) bool {
		if err != nil {
			return true
		}
		switch x.(type) {
		case *ArrayComprehension, *ObjectComprehension, *SetComprehension: // skip closures
			return true
		case Ref, Var:
			err = fmt.Errorf("default rule value cannot contain %v", TypeName(x))
			return true
		}
		return false
	})

	Walk(vis, term)

	if err != nil {
		return nil, err
	}

	loc := currentLocation(c)
	body := NewBody(NewExpr(BooleanTerm(true).SetLocation(loc)))

	rule := &Rule{
		Location: loc,
		Default:  true,
		Head: &Head{
			Location: currentLocation(c),
			Name:     name.(*Term).Value.(Var),
			Value:    value.(*Term),
		},
		Body: body,
	}
	rule.Body[0].Location = currentLocation(c)

	return []*Rule{rule}, nil
}

func (p *parser) callonDefaultRules1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onDefaultRules1(stack["name"], stack["value"])
}

func (c *current) onNormalRules1(head, b interface{}) (interface{}, error) {

	if head == nil {
		return nil, nil
	}

	sl := b.([]interface{})

	rules := []*Rule{
		&Rule{
			Location: currentLocation(c),
			Head:     head.(*Head),
			Body:     sl[0].(Body),
		},
	}

	var ordered bool
	prev := rules[0]

	for i, elem := range sl[1].([]interface{}) {

		next := elem.([]interface{})
		re := next[1].(ruleExt)

		if re.term == nil {
			if ordered {
				return nil, fmt.Errorf("expected 'else' keyword")
			}
			rules = append(rules, &Rule{
				Location: re.loc,
				Head:     prev.Head.Copy(),
				Body:     re.body,
			})
		} else {
			if (rules[0].Head.DocKind() != CompleteDoc) || (i != 0 && !ordered) {
				return nil, fmt.Errorf("unexpected 'else' keyword")
			}
			ordered = true
			curr := &Rule{
				Location: re.loc,
				Head: &Head{
					Name:     prev.Head.Name,
					Args:     prev.Head.Args.Copy(),
					Value:    re.term,
					Location: re.term.Location,
				},
				Body: re.body,
			}
			prev.Else = curr
			prev = curr
		}
	}

	return rules, nil
}

func (p *parser) callonNormalRules1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNormalRules1(stack["head"], stack["b"])
}

func (c *current) onRuleHead1(name, args, key, value interface{}) (interface{}, error) {

	head := &Head{}

	head.Location = currentLocation(c)
	head.Name = name.(*Term).Value.(Var)

	if args != nil && key != nil {
		return nil, fmt.Errorf("partial %v/%v %vs cannot take arguments", SetTypeName, ObjectTypeName, RuleTypeName)
	}

	if args != nil {
		argSlice := args.([]interface{})
		head.Args = argSlice[3].(Args)
	}

	if key != nil {
		keySlice := key.([]interface{})
		// Head definition above describes the "key" slice. We care about the "Term" element.
		head.Key = keySlice[3].(*Term)
	}

	if value != nil {
		valueSlice := value.([]interface{})
		// Head definition above describes the "value" slice. We care about the "Term" element.
		head.Value = valueSlice[len(valueSlice)-1].(*Term)
	}

	if key == nil && value == nil {
		head.Value = BooleanTerm(true).SetLocation(head.Location)
	}

	if key != nil && value != nil {
		switch head.Key.Value.(type) {
		case Var, String, Ref: // nop
		default:
			return nil, fmt.Errorf("object key must be one of %v, %v, %v not %v", StringTypeName, VarTypeName, RefTypeName, TypeName(head.Key.Value))
		}
	}

	return head, nil
}

func (p *parser) callonRuleHead1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRuleHead1(stack["name"], stack["args"], stack["key"], stack["value"])
}

func (c *current) onArgs1(head, tail interface{}) (interface{}, error) {
	return makeArgs(head, tail, currentLocation(c))
}

func (p *parser) callonArgs1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArgs1(stack["head"], stack["tail"])
}

func (c *current) onElse1(val, b interface{}) (interface{}, error) {
	bs := b.([]interface{})
	body := bs[1].(Body)

	if val == nil {
		term := BooleanTerm(true)
		term.Location = currentLocation(c)
		return ruleExt{term.Location, term, body}, nil
	}

	vs := val.([]interface{})
	t := vs[3].(*Term)
	return ruleExt{currentLocation(c), t, body}, nil
}

func (p *parser) callonElse1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onElse1(stack["val"], stack["b"])
}

func (c *current) onRuleDup1(b interface{}) (interface{}, error) {
	return ruleExt{loc: currentLocation(c), body: b.(Body)}, nil
}

func (p *parser) callonRuleDup1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRuleDup1(stack["b"])
}

func (c *current) onNonEmptyBraceEnclosedBody1(val interface{}) (interface{}, error) {
	if val == nil {
		panic("body must be non-empty")
	}
	return val, nil
}

func (p *parser) callonNonEmptyBraceEnclosedBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNonEmptyBraceEnclosedBody1(stack["val"])
}

func (c *current) onBraceEnclosedBody1(val interface{}) (interface{}, error) {

	if val == nil {
		loc := currentLocation(c)
		body := NewBody(NewExpr(ObjectTerm().SetLocation(loc)))
		body[0].Location = loc
		return body, nil
	}

	return val, nil
}

func (p *parser) callonBraceEnclosedBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBraceEnclosedBody1(stack["val"])
}

func (c *current) onWhitespaceBody1(head, tail interface{}) (interface{}, error) {
	return ifacesToBody(head, tail.([]interface{})...), nil
}

func (p *parser) callonWhitespaceBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onWhitespaceBody1(stack["head"], stack["tail"])
}

func (c *current) onNonWhitespaceBody1(head, tail interface{}) (interface{}, error) {
	return ifacesToBody(head, tail.([]interface{})...), nil
}

func (p *parser) callonNonWhitespaceBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNonWhitespaceBody1(stack["head"], stack["tail"])
}

func (c *current) onLiteral1(neg, val, with interface{}) (interface{}, error) {
	var expr *Expr
	switch val := val.(type) {
	case *Expr:
		expr = val
	case *Term:
		expr = &Expr{Terms: val}
	}
	expr.Location = currentLocation(c)
	expr.Negated = neg != nil

	if with != nil {
		sl := with.([]interface{})
		if head, ok := sl[1].(*With); ok {
			expr.With = []*With{head}
			if sl, ok := sl[2].([]interface{}); ok {
				for i := range sl {
					if w, ok := sl[i].([]interface{})[1].(*With); ok {
						expr.With = append(expr.With, w)
					}
				}
			}
		}
	}

	return expr, nil
}

func (p *parser) callonLiteral1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onLiteral1(stack["neg"], stack["val"], stack["with"])
}

func (c *current) onWith1(target, value interface{}) (interface{}, error) {
	with := &With{}
	with.Location = currentLocation(c)
	with.Target = target.(*Term)
	if err := IsValidImportPath(with.Target.Value); err != nil {
		return nil, err
	}
	with.Value = value.(*Term)
	return with, nil
}

func (p *parser) callonWith1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onWith1(stack["target"], stack["value"])
}

func (c *current) onInfixCallExpr1(output, operator, args interface{}) (interface{}, error) {
	return makeInfixCallExpr(operator, args, output)
}

func (p *parser) callonInfixCallExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onInfixCallExpr1(stack["output"], stack["operator"], stack["args"])
}

func (c *current) onInfixCallExprReverse1(operator, args, output interface{}) (interface{}, error) {
	return makeInfixCallExpr(operator, args, output)
}

func (p *parser) callonInfixCallExprReverse1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onInfixCallExprReverse1(stack["operator"], stack["args"], stack["output"])
}

func (c *current) onInfixArithExpr1(output, left, operator, right interface{}) (interface{}, error) {
	return makeInfixCallExpr(operator, Args{left.(*Term), right.(*Term)}, output)
}

func (p *parser) callonInfixArithExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onInfixArithExpr1(stack["output"], stack["left"], stack["operator"], stack["right"])
}

func (c *current) onInfixArithExprReverse1(left, operator, right, output interface{}) (interface{}, error) {
	return makeInfixCallExpr(operator, Args{left.(*Term), right.(*Term)}, output)
}

func (p *parser) callonInfixArithExprReverse1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onInfixArithExprReverse1(stack["left"], stack["operator"], stack["right"], stack["output"])
}

func (c *current) onArithInfixOp1(val interface{}) (interface{}, error) {
	op := string(c.text)
	for _, b := range Builtins {
		if string(b.Infix) == op {
			op = string(b.Name)
		}
	}
	loc := currentLocation(c)
	operator := RefTerm(VarTerm(op).SetLocation(loc)).SetLocation(loc)
	return operator, nil
}

func (p *parser) callonArithInfixOp1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArithInfixOp1(stack["val"])
}

func (c *current) onInfixRelationExpr1(left, operator, right interface{}) (interface{}, error) {
	return &Expr{
		Terms: []*Term{
			operator.(*Term),
			left.(*Term),
			right.(*Term),
		},
		Infix: true,
	}, nil
}

func (p *parser) callonInfixRelationExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onInfixRelationExpr1(stack["left"], stack["operator"], stack["right"])
}

func (c *current) onInfixRelationOp1(val interface{}) (interface{}, error) {
	op := string(c.text)
	for _, b := range Builtins {
		if string(b.Infix) == op {
			op = string(b.Name)
		}
	}
	loc := currentLocation(c)
	operator := RefTerm(VarTerm(op).SetLocation(loc)).SetLocation(loc)
	return operator, nil
}

func (p *parser) callonInfixRelationOp1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onInfixRelationOp1(stack["val"])
}

func (c *current) onCall1(name, head, tail interface{}) (interface{}, error) {
	buf := []*Term{name.(*Term)}
	if head == nil {
		return &Expr{Terms: buf}, nil
	}

	buf = append(buf, head.(*Term))

	// PrefixExpr above describes the "tail" structure. We only care about the "Term" elements.
	tailSlice := tail.([]interface{})
	for _, v := range tailSlice {
		s := v.([]interface{})
		buf = append(buf, s[len(s)-1].(*Term))
	}

	return &Expr{Terms: buf}, nil
}

func (p *parser) callonCall1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onCall1(stack["name"], stack["head"], stack["tail"])
}

func (c *current) onOperator1(val interface{}) (interface{}, error) {
	term := val.(*Term)
	switch term.Value.(type) {
	case Ref:
		return val, nil
	case Var:
		return RefTerm(term).SetLocation(currentLocation(c)), nil
	default:
		panic("unreachable")
	}
}

func (p *parser) callonOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onOperator1(stack["val"])
}

func (c *current) onTerm1(val interface{}) (interface{}, error) {
	return val, nil
}

func (p *parser) callonTerm1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onTerm1(stack["val"])
}

func (c *current) onArrayComprehension1(term, body interface{}) (interface{}, error) {
	ac := ArrayComprehensionTerm(term.(*Term), body.(Body))
	ac.Location = currentLocation(c)
	return ac, nil
}

func (p *parser) callonArrayComprehension1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArrayComprehension1(stack["term"], stack["body"])
}

func (c *current) onObjectComprehension1(key, value, body interface{}) (interface{}, error) {
	oc := ObjectComprehensionTerm(key.(*Term), value.(*Term), body.(Body))
	oc.Location = currentLocation(c)
	return oc, nil
}

func (p *parser) callonObjectComprehension1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onObjectComprehension1(stack["key"], stack["value"], stack["body"])
}

func (c *current) onSetComprehension1(term, body interface{}) (interface{}, error) {
	sc := SetComprehensionTerm(term.(*Term), body.(Body))
	sc.Location = currentLocation(c)
	return sc, nil
}

func (p *parser) callonSetComprehension1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onSetComprehension1(stack["term"], stack["body"])
}

func (c *current) onObject1(head, tail interface{}) (interface{}, error) {
	return makeObject(head, tail, currentLocation(c))
}

func (p *parser) callonObject1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onObject1(stack["head"], stack["tail"])
}

func (c *current) onArray1(head, tail interface{}) (interface{}, error) {
	return makeArray(head, tail, currentLocation(c))
}

func (p *parser) callonArray1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArray1(stack["head"], stack["tail"])
}

func (c *current) onSetEmpty1() (interface{}, error) {
	set := SetTerm()
	set.Location = currentLocation(c)
	return set, nil
}

func (p *parser) callonSetEmpty1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onSetEmpty1()
}

func (c *current) onSetNonEmpty1(head, tail interface{}) (interface{}, error) {
	set := SetTerm()
	set.Location = currentLocation(c)

	val := set.Value.(*Set)
	val.Add(head.(*Term))

	tailSlice := tail.([]interface{})
	for _, v := range tailSlice {
		s := v.([]interface{})
		// SetNonEmpty definition above describes the "tail" structure. We only care about the "Term" elements.
		val.Add(s[len(s)-1].(*Term))
	}

	return set, nil
}

func (p *parser) callonSetNonEmpty1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onSetNonEmpty1(stack["head"], stack["tail"])
}

func (c *current) onRef1(head, tail interface{}) (interface{}, error) {

	ref := RefTerm(head.(*Term))
	ref.Location = currentLocation(c)

	tailSlice := tail.([]interface{})
	for _, v := range tailSlice {
		ref.Value = append(ref.Value.(Ref), v.(*Term))
	}

	return ref, nil
}

func (p *parser) callonRef1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRef1(stack["head"], stack["tail"])
}

func (c *current) onRefDot1(val interface{}) (interface{}, error) {
	// Convert the Var into a string because 'foo.bar.baz' is equivalent to 'foo["bar"]["baz"]'.
	str := StringTerm(string(val.(*Term).Value.(Var)))
	str.Location = currentLocation(c)
	return str, nil
}

func (p *parser) callonRefDot1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRefDot1(stack["val"])
}

func (c *current) onRefBracket1(val interface{}) (interface{}, error) {
	return val, nil
}

func (p *parser) callonRefBracket1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRefBracket1(stack["val"])
}

func (c *current) onVar1(val interface{}) (interface{}, error) {
	return val.([]interface{})[0], nil
}

func (p *parser) callonVar1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onVar1(stack["val"])
}

func (c *current) onVarChecked4(val interface{}) (bool, error) {
	return IsKeyword(string(val.(*Term).Value.(Var))), nil
}

func (p *parser) callonVarChecked4() (bool, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onVarChecked4(stack["val"])
}

func (c *current) onVarUnchecked1() (interface{}, error) {
	str := string(c.text)
	variable := VarTerm(str)
	variable.Location = currentLocation(c)
	return variable, nil
}

func (p *parser) callonVarUnchecked1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onVarUnchecked1()
}

func (c *current) onNumber1() (interface{}, error) {
	f, ok := new(big.Float).SetString(string(c.text))
	if !ok {
		// This indicates the grammar is out-of-sync with what the string
		// representation of floating point numbers. This should not be
		// possible.
		panic("illegal value")
	}
	num := NumberTerm(json.Number(f.String()))
	num.Location = currentLocation(c)
	return num, nil
}

func (p *parser) callonNumber1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNumber1()
}

func (c *current) onQuotedString1() (interface{}, error) {
	var v string
	err := json.Unmarshal([]byte(c.text), &v)
	str := StringTerm(v)
	str.Location = currentLocation(c)
	return str, err
}

func (p *parser) callonQuotedString1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onQuotedString1()
}

func (c *current) onRawString1() (interface{}, error) {
	s := string(c.text)
	s = s[1 : len(s)-1] // Trim surrounding quotes.

	str := StringTerm(s)
	str.Location = currentLocation(c)
	return str, nil
}

func (p *parser) callonRawString1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRawString1()
}

func (c *current) onBool2() (interface{}, error) {
	bol := BooleanTerm(true)
	bol.Location = currentLocation(c)
	return bol, nil
}

func (p *parser) callonBool2() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBool2()
}

func (c *current) onBool4() (interface{}, error) {
	bol := BooleanTerm(false)
	bol.Location = currentLocation(c)
	return bol, nil
}

func (p *parser) callonBool4() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBool4()
}

func (c *current) onNull1() (interface{}, error) {
	null := NullTerm()
	null.Location = currentLocation(c)
	return null, nil
}

func (p *parser) callonNull1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNull1()
}

func (c *current) onComment1(text interface{}) (interface{}, error) {
	comment := NewComment(ifaceSliceToByteSlice(text))
	comment.Location = currentLocation(c)

	comments := c.globalStore[commentsKey].([]*Comment)
	comments = append(comments, comment)
	c.globalStore[commentsKey] = comments

	return comment, nil
}

func (p *parser) callonComment1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onComment1(stack["text"])
}

var (
	// errNoRule is returned when the grammar to parse has no rule.
	errNoRule = errors.New("grammar has no rule")

	// errInvalidEncoding is returned when the source is not properly
	// utf8-encoded.
	errInvalidEncoding = errors.New("invalid encoding")
)

// Option is a function that can set an option on the parser. It returns
// the previous setting as an Option.
type Option func(*parser) Option

// Debug creates an Option to set the debug flag to b. When set to true,
// debugging information is printed to stdout while parsing.
//
// The default is false.
func Debug(b bool) Option {
	return func(p *parser) Option {
		old := p.debug
		p.debug = b
		return Debug(old)
	}
}

// Memoize creates an Option to set the memoize flag to b. When set to true,
// the parser will cache all results so each expression is evaluated only
// once. This guarantees linear parsing time even for pathological cases,
// at the expense of more memory and slower times for typical cases.
//
// The default is false.
func Memoize(b bool) Option {
	return func(p *parser) Option {
		old := p.memoize
		p.memoize = b
		return Memoize(old)
	}
}

// Recover creates an Option to set the recover flag to b. When set to
// true, this causes the parser to recover from panics and convert it
// to an error. Setting it to false can be useful while debugging to
// access the full stack trace.
//
// The default is true.
func Recover(b bool) Option {
	return func(p *parser) Option {
		old := p.recover
		p.recover = b
		return Recover(old)
	}
}

// GlobalStore creates an Option to set a key to a certain value in
// the globalStore.
func GlobalStore(key string, value interface{}) Option {
	return func(p *parser) Option {
		old := p.cur.globalStore[key]
		p.cur.globalStore[key] = value
		return GlobalStore(key, old)
	}
}

// ParseFile parses the file identified by filename.
func ParseFile(filename string, opts ...Option) (i interface{}, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = closeErr
		}
	}()
	return ParseReader(filename, f, opts...)
}

// ParseReader parses the data from r using filename as information in the
// error messages.
func ParseReader(filename string, r io.Reader, opts ...Option) (interface{}, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return Parse(filename, b, opts...)
}

// Parse parses the data from b using filename as information in the
// error messages.
func Parse(filename string, b []byte, opts ...Option) (interface{}, error) {
	return newParser(filename, b, opts...).parse(g)
}

// position records a position in the text.
type position struct {
	line, col, offset int
}

func (p position) String() string {
	return fmt.Sprintf("%d:%d [%d]", p.line, p.col, p.offset)
}

// savepoint stores all state required to go back to this point in the
// parser.
type savepoint struct {
	position
	rn rune
	w  int
}

type current struct {
	pos  position // start position of the match
	text []byte   // raw text of the match

	// the globalStore allows the parser to store arbitrary values
	globalStore map[string]interface{}
}

// the AST types...

type grammar struct {
	pos   position
	rules []*rule
}

type rule struct {
	pos         position
	name        string
	displayName string
	expr        interface{}
}

type choiceExpr struct {
	pos          position
	alternatives []interface{}
}

type actionExpr struct {
	pos  position
	expr interface{}
	run  func(*parser) (interface{}, error)
}

type seqExpr struct {
	pos   position
	exprs []interface{}
}

type labeledExpr struct {
	pos   position
	label string
	expr  interface{}
}

type expr struct {
	pos  position
	expr interface{}
}

type andExpr expr
type notExpr expr
type zeroOrOneExpr expr
type zeroOrMoreExpr expr
type oneOrMoreExpr expr

type ruleRefExpr struct {
	pos  position
	name string
}

type andCodeExpr struct {
	pos position
	run func(*parser) (bool, error)
}

type notCodeExpr struct {
	pos position
	run func(*parser) (bool, error)
}

type litMatcher struct {
	pos        position
	val        string
	ignoreCase bool
}

type charClassMatcher struct {
	pos             position
	val             string
	basicLatinChars [128]bool
	chars           []rune
	ranges          []rune
	classes         []*unicode.RangeTable
	ignoreCase      bool
	inverted        bool
}

type anyMatcher position

// errList cumulates the errors found by the parser.
type errList []error

func (e *errList) add(err error) {
	*e = append(*e, err)
}

func (e errList) err() error {
	if len(e) == 0 {
		return nil
	}
	e.dedupe()
	return e
}

func (e *errList) dedupe() {
	var cleaned []error
	set := make(map[string]bool)
	for _, err := range *e {
		if msg := err.Error(); !set[msg] {
			set[msg] = true
			cleaned = append(cleaned, err)
		}
	}
	*e = cleaned
}

func (e errList) Error() string {
	switch len(e) {
	case 0:
		return ""
	case 1:
		return e[0].Error()
	default:
		var buf bytes.Buffer

		for i, err := range e {
			if i > 0 {
				buf.WriteRune('\n')
			}
			buf.WriteString(err.Error())
		}
		return buf.String()
	}
}

// parserError wraps an error with a prefix indicating the rule in which
// the error occurred. The original error is stored in the Inner field.
type parserError struct {
	Inner    error
	pos      position
	prefix   string
	expected []string
}

// Error returns the error message.
func (p *parserError) Error() string {
	return p.prefix + ": " + p.Inner.Error()
}

// newParser creates a parser with the specified input source and options.
func newParser(filename string, b []byte, opts ...Option) *parser {
	p := &parser{
		filename: filename,
		errs:     new(errList),
		data:     b,
		pt:       savepoint{position: position{line: 1}},
		recover:  true,
		cur: current{
			globalStore: make(map[string]interface{}),
		},
		maxFailPos:      position{col: 1, line: 1},
		maxFailExpected: make([]string, 0, 20),
	}
	p.setOptions(opts)
	return p
}

// setOptions applies the options to the parser.
func (p *parser) setOptions(opts []Option) {
	for _, opt := range opts {
		opt(p)
	}
}

type resultTuple struct {
	v   interface{}
	b   bool
	end savepoint
}

type parser struct {
	filename string
	pt       savepoint
	cur      current

	data []byte
	errs *errList

	depth   int
	recover bool
	debug   bool

	memoize bool
	// memoization table for the packrat algorithm:
	// map[offset in source] map[expression or rule] {value, match}
	memo map[int]map[interface{}]resultTuple

	// rules table, maps the rule identifier to the rule node
	rules map[string]*rule
	// variables stack, map of label to value
	vstack []map[string]interface{}
	// rule stack, allows identification of the current rule in errors
	rstack []*rule

	// stats
	exprCnt int

	// parse fail
	maxFailPos            position
	maxFailExpected       []string
	maxFailInvertExpected bool
}

// push a variable set on the vstack.
func (p *parser) pushV() {
	if cap(p.vstack) == len(p.vstack) {
		// create new empty slot in the stack
		p.vstack = append(p.vstack, nil)
	} else {
		// slice to 1 more
		p.vstack = p.vstack[:len(p.vstack)+1]
	}

	// get the last args set
	m := p.vstack[len(p.vstack)-1]
	if m != nil && len(m) == 0 {
		// empty map, all good
		return
	}

	m = make(map[string]interface{})
	p.vstack[len(p.vstack)-1] = m
}

// pop a variable set from the vstack.
func (p *parser) popV() {
	// if the map is not empty, clear it
	m := p.vstack[len(p.vstack)-1]
	if len(m) > 0 {
		// GC that map
		p.vstack[len(p.vstack)-1] = nil
	}
	p.vstack = p.vstack[:len(p.vstack)-1]
}

func (p *parser) print(prefix, s string) string {
	if !p.debug {
		return s
	}

	fmt.Printf("%s %d:%d:%d: %s [%#U]\n",
		prefix, p.pt.line, p.pt.col, p.pt.offset, s, p.pt.rn)
	return s
}

func (p *parser) in(s string) string {
	p.depth++
	return p.print(strings.Repeat(" ", p.depth)+">", s)
}

func (p *parser) out(s string) string {
	p.depth--
	return p.print(strings.Repeat(" ", p.depth)+"<", s)
}

func (p *parser) addErr(err error) {
	p.addErrAt(err, p.pt.position, []string{})
}

func (p *parser) addErrAt(err error, pos position, expected []string) {
	var buf bytes.Buffer
	if p.filename != "" {
		buf.WriteString(p.filename)
	}
	if buf.Len() > 0 {
		buf.WriteString(":")
	}
	buf.WriteString(fmt.Sprintf("%d:%d (%d)", pos.line, pos.col, pos.offset))
	if len(p.rstack) > 0 {
		if buf.Len() > 0 {
			buf.WriteString(": ")
		}
		rule := p.rstack[len(p.rstack)-1]
		if rule.displayName != "" {
			buf.WriteString("rule " + rule.displayName)
		} else {
			buf.WriteString("rule " + rule.name)
		}
	}
	pe := &parserError{Inner: err, pos: pos, prefix: buf.String(), expected: expected}
	p.errs.add(pe)
}

func (p *parser) failAt(fail bool, pos position, want string) {
	// process fail if parsing fails and not inverted or parsing succeeds and invert is set
	if fail == p.maxFailInvertExpected {
		if pos.offset < p.maxFailPos.offset {
			return
		}

		if pos.offset > p.maxFailPos.offset {
			p.maxFailPos = pos
			p.maxFailExpected = p.maxFailExpected[:0]
		}

		if p.maxFailInvertExpected {
			want = "!" + want
		}
		p.maxFailExpected = append(p.maxFailExpected, want)
	}
}

// read advances the parser to the next rune.
func (p *parser) read() {
	p.pt.offset += p.pt.w
	rn, n := utf8.DecodeRune(p.data[p.pt.offset:])
	p.pt.rn = rn
	p.pt.w = n
	p.pt.col++
	if rn == '\n' {
		p.pt.line++
		p.pt.col = 0
	}

	if rn == utf8.RuneError {
		if n == 1 {
			p.addErr(errInvalidEncoding)
		}
	}
}

// restore parser position to the savepoint pt.
func (p *parser) restore(pt savepoint) {
	if p.debug {
		defer p.out(p.in("restore"))
	}
	if pt.offset == p.pt.offset {
		return
	}
	p.pt = pt
}

// get the slice of bytes from the savepoint start to the current position.
func (p *parser) sliceFrom(start savepoint) []byte {
	return p.data[start.position.offset:p.pt.position.offset]
}

func (p *parser) getMemoized(node interface{}) (resultTuple, bool) {
	if len(p.memo) == 0 {
		return resultTuple{}, false
	}
	m := p.memo[p.pt.offset]
	if len(m) == 0 {
		return resultTuple{}, false
	}
	res, ok := m[node]
	return res, ok
}

func (p *parser) setMemoized(pt savepoint, node interface{}, tuple resultTuple) {
	if p.memo == nil {
		p.memo = make(map[int]map[interface{}]resultTuple)
	}
	m := p.memo[pt.offset]
	if m == nil {
		m = make(map[interface{}]resultTuple)
		p.memo[pt.offset] = m
	}
	m[node] = tuple
}

func (p *parser) buildRulesTable(g *grammar) {
	p.rules = make(map[string]*rule, len(g.rules))
	for _, r := range g.rules {
		p.rules[r.name] = r
	}
}

func (p *parser) parse(g *grammar) (val interface{}, err error) {
	if len(g.rules) == 0 {
		p.addErr(errNoRule)
		return nil, p.errs.err()
	}

	// TODO : not super critical but this could be generated
	p.buildRulesTable(g)

	if p.recover {
		// panic can be used in action code to stop parsing immediately
		// and return the panic as an error.
		defer func() {
			if e := recover(); e != nil {
				if p.debug {
					defer p.out(p.in("panic handler"))
				}
				val = nil
				switch e := e.(type) {
				case error:
					p.addErr(e)
				default:
					p.addErr(fmt.Errorf("%v", e))
				}
				err = p.errs.err()
			}
		}()
	}

	// start rule is rule [0]
	p.read() // advance to first rune
	val, ok := p.parseRule(g.rules[0])
	if !ok {
		if len(*p.errs) == 0 {
			// If parsing fails, but no errors have been recorded, the expected values
			// for the farthest parser position are returned as error.
			maxFailExpectedMap := make(map[string]struct{}, len(p.maxFailExpected))
			for _, v := range p.maxFailExpected {
				maxFailExpectedMap[v] = struct{}{}
			}
			expected := make([]string, 0, len(maxFailExpectedMap))
			eof := false
			if _, ok := maxFailExpectedMap["!."]; ok {
				delete(maxFailExpectedMap, "!.")
				eof = true
			}
			for k := range maxFailExpectedMap {
				expected = append(expected, k)
			}
			sort.Strings(expected)
			if eof {
				expected = append(expected, "EOF")
			}
			p.addErrAt(errors.New("no match found, expected: "+listJoin(expected, ", ", "or")), p.maxFailPos, expected)
		}
		return nil, p.errs.err()
	}
	return val, p.errs.err()
}

func listJoin(list []string, sep string, lastSep string) string {
	switch len(list) {
	case 0:
		return ""
	case 1:
		return list[0]
	default:
		return fmt.Sprintf("%s %s %s", strings.Join(list[:len(list)-1], sep), lastSep, list[len(list)-1])
	}
}

func (p *parser) parseRule(rule *rule) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseRule " + rule.name))
	}

	if p.memoize {
		res, ok := p.getMemoized(rule)
		if ok {
			p.restore(res.end)
			return res.v, res.b
		}
	}

	start := p.pt
	p.rstack = append(p.rstack, rule)
	p.pushV()
	val, ok := p.parseExpr(rule.expr)
	p.popV()
	p.rstack = p.rstack[:len(p.rstack)-1]
	if ok && p.debug {
		p.print(strings.Repeat(" ", p.depth)+"MATCH", string(p.sliceFrom(start)))
	}

	if p.memoize {
		p.setMemoized(start, rule, resultTuple{val, ok, p.pt})
	}
	return val, ok
}

func (p *parser) parseExpr(expr interface{}) (interface{}, bool) {
	var pt savepoint

	if p.memoize {
		res, ok := p.getMemoized(expr)
		if ok {
			p.restore(res.end)
			return res.v, res.b
		}
		pt = p.pt
	}

	p.exprCnt++
	var val interface{}
	var ok bool
	switch expr := expr.(type) {
	case *actionExpr:
		val, ok = p.parseActionExpr(expr)
	case *andCodeExpr:
		val, ok = p.parseAndCodeExpr(expr)
	case *andExpr:
		val, ok = p.parseAndExpr(expr)
	case *anyMatcher:
		val, ok = p.parseAnyMatcher(expr)
	case *charClassMatcher:
		val, ok = p.parseCharClassMatcher(expr)
	case *choiceExpr:
		val, ok = p.parseChoiceExpr(expr)
	case *labeledExpr:
		val, ok = p.parseLabeledExpr(expr)
	case *litMatcher:
		val, ok = p.parseLitMatcher(expr)
	case *notCodeExpr:
		val, ok = p.parseNotCodeExpr(expr)
	case *notExpr:
		val, ok = p.parseNotExpr(expr)
	case *oneOrMoreExpr:
		val, ok = p.parseOneOrMoreExpr(expr)
	case *ruleRefExpr:
		val, ok = p.parseRuleRefExpr(expr)
	case *seqExpr:
		val, ok = p.parseSeqExpr(expr)
	case *zeroOrMoreExpr:
		val, ok = p.parseZeroOrMoreExpr(expr)
	case *zeroOrOneExpr:
		val, ok = p.parseZeroOrOneExpr(expr)
	default:
		panic(fmt.Sprintf("unknown expression type %T", expr))
	}
	if p.memoize {
		p.setMemoized(pt, expr, resultTuple{val, ok, p.pt})
	}
	return val, ok
}

func (p *parser) parseActionExpr(act *actionExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseActionExpr"))
	}

	start := p.pt
	val, ok := p.parseExpr(act.expr)
	if ok {
		p.cur.pos = start.position
		p.cur.text = p.sliceFrom(start)
		actVal, err := act.run(p)
		if err != nil {
			p.addErrAt(err, start.position, []string{})
		}
		val = actVal
	}
	if ok && p.debug {
		p.print(strings.Repeat(" ", p.depth)+"MATCH", string(p.sliceFrom(start)))
	}
	return val, ok
}

func (p *parser) parseAndCodeExpr(and *andCodeExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseAndCodeExpr"))
	}

	ok, err := and.run(p)
	if err != nil {
		p.addErr(err)
	}
	return nil, ok
}

func (p *parser) parseAndExpr(and *andExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseAndExpr"))
	}

	pt := p.pt
	p.pushV()
	_, ok := p.parseExpr(and.expr)
	p.popV()
	p.restore(pt)
	return nil, ok
}

func (p *parser) parseAnyMatcher(any *anyMatcher) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseAnyMatcher"))
	}

	if p.pt.rn != utf8.RuneError {
		start := p.pt
		p.read()
		p.failAt(true, start.position, ".")
		return p.sliceFrom(start), true
	}
	p.failAt(false, p.pt.position, ".")
	return nil, false
}

func (p *parser) parseCharClassMatcher(chr *charClassMatcher) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseCharClassMatcher"))
	}

	cur := p.pt.rn
	start := p.pt

	// can't match EOF
	if cur == utf8.RuneError {
		p.failAt(false, start.position, chr.val)
		return nil, false
	}

	if chr.ignoreCase {
		cur = unicode.ToLower(cur)
	}

	// try to match in the list of available chars
	for _, rn := range chr.chars {
		if rn == cur {
			if chr.inverted {
				p.failAt(false, start.position, chr.val)
				return nil, false
			}
			p.read()
			p.failAt(true, start.position, chr.val)
			return p.sliceFrom(start), true
		}
	}

	// try to match in the list of ranges
	for i := 0; i < len(chr.ranges); i += 2 {
		if cur >= chr.ranges[i] && cur <= chr.ranges[i+1] {
			if chr.inverted {
				p.failAt(false, start.position, chr.val)
				return nil, false
			}
			p.read()
			p.failAt(true, start.position, chr.val)
			return p.sliceFrom(start), true
		}
	}

	// try to match in the list of Unicode classes
	for _, cl := range chr.classes {
		if unicode.Is(cl, cur) {
			if chr.inverted {
				p.failAt(false, start.position, chr.val)
				return nil, false
			}
			p.read()
			p.failAt(true, start.position, chr.val)
			return p.sliceFrom(start), true
		}
	}

	if chr.inverted {
		p.read()
		p.failAt(true, start.position, chr.val)
		return p.sliceFrom(start), true
	}
	p.failAt(false, start.position, chr.val)
	return nil, false
}

func (p *parser) parseChoiceExpr(ch *choiceExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseChoiceExpr"))
	}

	for _, alt := range ch.alternatives {
		p.pushV()
		val, ok := p.parseExpr(alt)
		p.popV()
		if ok {
			return val, ok
		}
	}
	return nil, false
}

func (p *parser) parseLabeledExpr(lab *labeledExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseLabeledExpr"))
	}

	p.pushV()
	val, ok := p.parseExpr(lab.expr)
	p.popV()
	if ok && lab.label != "" {
		m := p.vstack[len(p.vstack)-1]
		m[lab.label] = val
	}
	return val, ok
}

func (p *parser) parseLitMatcher(lit *litMatcher) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseLitMatcher"))
	}

	ignoreCase := ""
	if lit.ignoreCase {
		ignoreCase = "i"
	}
	val := fmt.Sprintf("%q%s", lit.val, ignoreCase)
	start := p.pt
	for _, want := range lit.val {
		cur := p.pt.rn
		if lit.ignoreCase {
			cur = unicode.ToLower(cur)
		}
		if cur != want {
			p.failAt(false, start.position, val)
			p.restore(start)
			return nil, false
		}
		p.read()
	}
	p.failAt(true, start.position, val)
	return p.sliceFrom(start), true
}

func (p *parser) parseNotCodeExpr(not *notCodeExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseNotCodeExpr"))
	}

	ok, err := not.run(p)
	if err != nil {
		p.addErr(err)
	}
	return nil, !ok
}

func (p *parser) parseNotExpr(not *notExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseNotExpr"))
	}

	pt := p.pt
	p.pushV()
	p.maxFailInvertExpected = !p.maxFailInvertExpected
	_, ok := p.parseExpr(not.expr)
	p.maxFailInvertExpected = !p.maxFailInvertExpected
	p.popV()
	p.restore(pt)
	return nil, !ok
}

func (p *parser) parseOneOrMoreExpr(expr *oneOrMoreExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseOneOrMoreExpr"))
	}

	var vals []interface{}

	for {
		p.pushV()
		val, ok := p.parseExpr(expr.expr)
		p.popV()
		if !ok {
			if len(vals) == 0 {
				// did not match once, no match
				return nil, false
			}
			return vals, true
		}
		vals = append(vals, val)
	}
}

func (p *parser) parseRuleRefExpr(ref *ruleRefExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseRuleRefExpr " + ref.name))
	}

	if ref.name == "" {
		panic(fmt.Sprintf("%s: invalid rule: missing name", ref.pos))
	}

	rule := p.rules[ref.name]
	if rule == nil {
		p.addErr(fmt.Errorf("undefined rule: %s", ref.name))
		return nil, false
	}
	return p.parseRule(rule)
}

func (p *parser) parseSeqExpr(seq *seqExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseSeqExpr"))
	}

	vals := make([]interface{}, 0, len(seq.exprs))

	pt := p.pt
	for _, expr := range seq.exprs {
		val, ok := p.parseExpr(expr)
		if !ok {
			p.restore(pt)
			return nil, false
		}
		vals = append(vals, val)
	}
	return vals, true
}

func (p *parser) parseZeroOrMoreExpr(expr *zeroOrMoreExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseZeroOrMoreExpr"))
	}

	var vals []interface{}

	for {
		p.pushV()
		val, ok := p.parseExpr(expr.expr)
		p.popV()
		if !ok {
			return vals, true
		}
		vals = append(vals, val)
	}
}

func (p *parser) parseZeroOrOneExpr(expr *zeroOrOneExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseZeroOrOneExpr"))
	}

	p.pushV()
	val, _ := p.parseExpr(expr.expr)
	p.popV()
	// whether it matched or not, consider it a match
	return val, true
}
