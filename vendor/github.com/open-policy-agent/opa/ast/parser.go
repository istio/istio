package ast

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var g = &grammar{
	rules: []*rule{
		{
			name: "Program",
			pos:  position{line: 5, col: 1, offset: 17},
			expr: &actionExpr{
				pos: position{line: 5, col: 12, offset: 28},
				run: (*parser).callonProgram1,
				expr: &seqExpr{
					pos: position{line: 5, col: 12, offset: 28},
					exprs: []interface{}{
						&ruleRefExpr{
							pos:  position{line: 5, col: 12, offset: 28},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 5, col: 14, offset: 30},
							label: "vals",
							expr: &zeroOrOneExpr{
								pos: position{line: 5, col: 19, offset: 35},
								expr: &seqExpr{
									pos: position{line: 5, col: 20, offset: 36},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 5, col: 20, offset: 36},
											name: "Stmt",
										},
										&zeroOrMoreExpr{
											pos: position{line: 5, col: 25, offset: 41},
											expr: &seqExpr{
												pos: position{line: 5, col: 26, offset: 42},
												exprs: []interface{}{
													&ruleRefExpr{
														pos:  position{line: 5, col: 26, offset: 42},
														name: "ws",
													},
													&ruleRefExpr{
														pos:  position{line: 5, col: 29, offset: 45},
														name: "Stmt",
													},
												},
											},
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 5, col: 38, offset: 54},
							name: "_",
						},
						&ruleRefExpr{
							pos:  position{line: 5, col: 40, offset: 56},
							name: "EOF",
						},
					},
				},
			},
		},
		{
			name: "Stmt",
			pos:  position{line: 9, col: 1, offset: 97},
			expr: &actionExpr{
				pos: position{line: 9, col: 9, offset: 105},
				run: (*parser).callonStmt1,
				expr: &labeledExpr{
					pos:   position{line: 9, col: 9, offset: 105},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 9, col: 14, offset: 110},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 9, col: 14, offset: 110},
								name: "Package",
							},
							&ruleRefExpr{
								pos:  position{line: 9, col: 24, offset: 120},
								name: "Import",
							},
							&ruleRefExpr{
								pos:  position{line: 9, col: 33, offset: 129},
								name: "Rules",
							},
							&ruleRefExpr{
								pos:  position{line: 9, col: 41, offset: 137},
								name: "Body",
							},
							&ruleRefExpr{
								pos:  position{line: 9, col: 48, offset: 144},
								name: "Comment",
							},
						},
					},
				},
			},
		},
		{
			name: "Package",
			pos:  position{line: 13, col: 1, offset: 178},
			expr: &actionExpr{
				pos: position{line: 13, col: 12, offset: 189},
				run: (*parser).callonPackage1,
				expr: &seqExpr{
					pos: position{line: 13, col: 12, offset: 189},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 13, col: 12, offset: 189},
							val:        "package",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 13, col: 22, offset: 199},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 13, col: 25, offset: 202},
							label: "val",
							expr: &choiceExpr{
								pos: position{line: 13, col: 30, offset: 207},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 13, col: 30, offset: 207},
										name: "Ref",
									},
									&ruleRefExpr{
										pos:  position{line: 13, col: 36, offset: 213},
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
			pos:  position{line: 17, col: 1, offset: 279},
			expr: &actionExpr{
				pos: position{line: 17, col: 11, offset: 289},
				run: (*parser).callonImport1,
				expr: &seqExpr{
					pos: position{line: 17, col: 11, offset: 289},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 17, col: 11, offset: 289},
							val:        "import",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 17, col: 20, offset: 298},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 17, col: 23, offset: 301},
							label: "path",
							expr: &choiceExpr{
								pos: position{line: 17, col: 29, offset: 307},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 17, col: 29, offset: 307},
										name: "Ref",
									},
									&ruleRefExpr{
										pos:  position{line: 17, col: 35, offset: 313},
										name: "Var",
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 17, col: 40, offset: 318},
							label: "alias",
							expr: &zeroOrOneExpr{
								pos: position{line: 17, col: 46, offset: 324},
								expr: &seqExpr{
									pos: position{line: 17, col: 47, offset: 325},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 17, col: 47, offset: 325},
											name: "ws",
										},
										&litMatcher{
											pos:        position{line: 17, col: 50, offset: 328},
											val:        "as",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 17, col: 55, offset: 333},
											name: "ws",
										},
										&ruleRefExpr{
											pos:  position{line: 17, col: 58, offset: 336},
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
			pos:  position{line: 21, col: 1, offset: 402},
			expr: &choiceExpr{
				pos: position{line: 21, col: 10, offset: 411},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 21, col: 10, offset: 411},
						name: "DefaultRules",
					},
					&ruleRefExpr{
						pos:  position{line: 21, col: 25, offset: 426},
						name: "NormalRules",
					},
				},
			},
		},
		{
			name: "DefaultRules",
			pos:  position{line: 23, col: 1, offset: 439},
			expr: &actionExpr{
				pos: position{line: 23, col: 17, offset: 455},
				run: (*parser).callonDefaultRules1,
				expr: &seqExpr{
					pos: position{line: 23, col: 17, offset: 455},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 23, col: 17, offset: 455},
							val:        "default",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 23, col: 27, offset: 465},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 23, col: 30, offset: 468},
							label: "name",
							expr: &ruleRefExpr{
								pos:  position{line: 23, col: 35, offset: 473},
								name: "Var",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 23, col: 39, offset: 477},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 23, col: 41, offset: 479},
							val:        "=",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 23, col: 45, offset: 483},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 23, col: 47, offset: 485},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 23, col: 53, offset: 491},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "NormalRules",
			pos:  position{line: 27, col: 1, offset: 561},
			expr: &actionExpr{
				pos: position{line: 27, col: 16, offset: 576},
				run: (*parser).callonNormalRules1,
				expr: &seqExpr{
					pos: position{line: 27, col: 16, offset: 576},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 27, col: 16, offset: 576},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 27, col: 21, offset: 581},
								name: "RuleHead",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 27, col: 30, offset: 590},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 27, col: 32, offset: 592},
							label: "rest",
							expr: &seqExpr{
								pos: position{line: 27, col: 38, offset: 598},
								exprs: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 27, col: 38, offset: 598},
										name: "NonEmptyBraceEnclosedBody",
									},
									&zeroOrMoreExpr{
										pos: position{line: 27, col: 64, offset: 624},
										expr: &seqExpr{
											pos: position{line: 27, col: 66, offset: 626},
											exprs: []interface{}{
												&ruleRefExpr{
													pos:  position{line: 27, col: 66, offset: 626},
													name: "_",
												},
												&ruleRefExpr{
													pos:  position{line: 27, col: 68, offset: 628},
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
			pos:  position{line: 31, col: 1, offset: 697},
			expr: &actionExpr{
				pos: position{line: 31, col: 13, offset: 709},
				run: (*parser).callonRuleHead1,
				expr: &seqExpr{
					pos: position{line: 31, col: 13, offset: 709},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 31, col: 13, offset: 709},
							label: "name",
							expr: &ruleRefExpr{
								pos:  position{line: 31, col: 18, offset: 714},
								name: "Var",
							},
						},
						&labeledExpr{
							pos:   position{line: 31, col: 22, offset: 718},
							label: "args",
							expr: &zeroOrOneExpr{
								pos: position{line: 31, col: 27, offset: 723},
								expr: &seqExpr{
									pos: position{line: 31, col: 29, offset: 725},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 31, col: 29, offset: 725},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 31, col: 31, offset: 727},
											val:        "(",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 35, offset: 731},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 37, offset: 733},
											name: "Args",
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 42, offset: 738},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 31, col: 44, offset: 740},
											val:        ")",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 48, offset: 744},
											name: "_",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 31, col: 53, offset: 749},
							label: "key",
							expr: &zeroOrOneExpr{
								pos: position{line: 31, col: 57, offset: 753},
								expr: &seqExpr{
									pos: position{line: 31, col: 59, offset: 755},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 31, col: 59, offset: 755},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 31, col: 61, offset: 757},
											val:        "[",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 65, offset: 761},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 67, offset: 763},
											name: "ExprTerm",
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 76, offset: 772},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 31, col: 78, offset: 774},
											val:        "]",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 82, offset: 778},
											name: "_",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 31, col: 87, offset: 783},
							label: "value",
							expr: &zeroOrOneExpr{
								pos: position{line: 31, col: 93, offset: 789},
								expr: &seqExpr{
									pos: position{line: 31, col: 95, offset: 791},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 31, col: 95, offset: 791},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 31, col: 97, offset: 793},
											val:        "=",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 101, offset: 797},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 31, col: 103, offset: 799},
											name: "ExprTerm",
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
			pos:  position{line: 35, col: 1, offset: 884},
			expr: &actionExpr{
				pos: position{line: 35, col: 9, offset: 892},
				run: (*parser).callonArgs1,
				expr: &labeledExpr{
					pos:   position{line: 35, col: 9, offset: 892},
					label: "list",
					expr: &ruleRefExpr{
						pos:  position{line: 35, col: 14, offset: 897},
						name: "ExprTermList",
					},
				},
			},
		},
		{
			name: "Else",
			pos:  position{line: 39, col: 1, offset: 941},
			expr: &actionExpr{
				pos: position{line: 39, col: 9, offset: 949},
				run: (*parser).callonElse1,
				expr: &seqExpr{
					pos: position{line: 39, col: 9, offset: 949},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 39, col: 9, offset: 949},
							val:        "else",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 39, col: 16, offset: 956},
							label: "value",
							expr: &zeroOrOneExpr{
								pos: position{line: 39, col: 22, offset: 962},
								expr: &seqExpr{
									pos: position{line: 39, col: 24, offset: 964},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 39, col: 24, offset: 964},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 39, col: 26, offset: 966},
											val:        "=",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 39, col: 30, offset: 970},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 39, col: 32, offset: 972},
											name: "Term",
										},
									},
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 39, col: 40, offset: 980},
							label: "body",
							expr: &seqExpr{
								pos: position{line: 39, col: 47, offset: 987},
								exprs: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 39, col: 47, offset: 987},
										name: "_",
									},
									&ruleRefExpr{
										pos:  position{line: 39, col: 49, offset: 989},
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
			pos:  position{line: 43, col: 1, offset: 1078},
			expr: &actionExpr{
				pos: position{line: 43, col: 12, offset: 1089},
				run: (*parser).callonRuleDup1,
				expr: &labeledExpr{
					pos:   position{line: 43, col: 12, offset: 1089},
					label: "b",
					expr: &ruleRefExpr{
						pos:  position{line: 43, col: 14, offset: 1091},
						name: "NonEmptyBraceEnclosedBody",
					},
				},
			},
		},
		{
			name: "RuleExt",
			pos:  position{line: 47, col: 1, offset: 1187},
			expr: &choiceExpr{
				pos: position{line: 47, col: 12, offset: 1198},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 47, col: 12, offset: 1198},
						name: "Else",
					},
					&ruleRefExpr{
						pos:  position{line: 47, col: 19, offset: 1205},
						name: "RuleDup",
					},
				},
			},
		},
		{
			name: "Body",
			pos:  position{line: 49, col: 1, offset: 1214},
			expr: &choiceExpr{
				pos: position{line: 49, col: 9, offset: 1222},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 49, col: 9, offset: 1222},
						name: "NonWhitespaceBody",
					},
					&ruleRefExpr{
						pos:  position{line: 49, col: 29, offset: 1242},
						name: "BraceEnclosedBody",
					},
				},
			},
		},
		{
			name: "NonEmptyBraceEnclosedBody",
			pos:  position{line: 51, col: 1, offset: 1261},
			expr: &actionExpr{
				pos: position{line: 51, col: 30, offset: 1290},
				run: (*parser).callonNonEmptyBraceEnclosedBody1,
				expr: &seqExpr{
					pos: position{line: 51, col: 30, offset: 1290},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 51, col: 30, offset: 1290},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 51, col: 34, offset: 1294},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 51, col: 36, offset: 1296},
							label: "val",
							expr: &zeroOrOneExpr{
								pos: position{line: 51, col: 40, offset: 1300},
								expr: &ruleRefExpr{
									pos:  position{line: 51, col: 40, offset: 1300},
									name: "WhitespaceBody",
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 51, col: 56, offset: 1316},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 51, col: 58, offset: 1318},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "BraceEnclosedBody",
			pos:  position{line: 58, col: 1, offset: 1413},
			expr: &actionExpr{
				pos: position{line: 58, col: 22, offset: 1434},
				run: (*parser).callonBraceEnclosedBody1,
				expr: &seqExpr{
					pos: position{line: 58, col: 22, offset: 1434},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 58, col: 22, offset: 1434},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 58, col: 26, offset: 1438},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 58, col: 28, offset: 1440},
							label: "val",
							expr: &zeroOrOneExpr{
								pos: position{line: 58, col: 32, offset: 1444},
								expr: &ruleRefExpr{
									pos:  position{line: 58, col: 32, offset: 1444},
									name: "WhitespaceBody",
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 58, col: 48, offset: 1460},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 58, col: 50, offset: 1462},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "WhitespaceBody",
			pos:  position{line: 62, col: 1, offset: 1529},
			expr: &actionExpr{
				pos: position{line: 62, col: 19, offset: 1547},
				run: (*parser).callonWhitespaceBody1,
				expr: &seqExpr{
					pos: position{line: 62, col: 19, offset: 1547},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 62, col: 19, offset: 1547},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 62, col: 24, offset: 1552},
								name: "Literal",
							},
						},
						&labeledExpr{
							pos:   position{line: 62, col: 32, offset: 1560},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 62, col: 37, offset: 1565},
								expr: &seqExpr{
									pos: position{line: 62, col: 38, offset: 1566},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 62, col: 38, offset: 1566},
											name: "WhitespaceLiteralSeparator",
										},
										&ruleRefExpr{
											pos:  position{line: 62, col: 65, offset: 1593},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 62, col: 67, offset: 1595},
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
			pos:  position{line: 66, col: 1, offset: 1645},
			expr: &actionExpr{
				pos: position{line: 66, col: 22, offset: 1666},
				run: (*parser).callonNonWhitespaceBody1,
				expr: &seqExpr{
					pos: position{line: 66, col: 22, offset: 1666},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 66, col: 22, offset: 1666},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 66, col: 27, offset: 1671},
								name: "Literal",
							},
						},
						&labeledExpr{
							pos:   position{line: 66, col: 35, offset: 1679},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 66, col: 40, offset: 1684},
								expr: &seqExpr{
									pos: position{line: 66, col: 42, offset: 1686},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 66, col: 42, offset: 1686},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 66, col: 44, offset: 1688},
											name: "NonWhitespaceLiteralSeparator",
										},
										&ruleRefExpr{
											pos:  position{line: 66, col: 74, offset: 1718},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 66, col: 76, offset: 1720},
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
			name: "WhitespaceLiteralSeparator",
			pos:  position{line: 70, col: 1, offset: 1770},
			expr: &seqExpr{
				pos: position{line: 70, col: 31, offset: 1800},
				exprs: []interface{}{
					&zeroOrMoreExpr{
						pos: position{line: 70, col: 31, offset: 1800},
						expr: &charClassMatcher{
							pos:        position{line: 70, col: 31, offset: 1800},
							val:        "[ \\t]",
							chars:      []rune{' ', '\t'},
							ignoreCase: false,
							inverted:   false,
						},
					},
					&choiceExpr{
						pos: position{line: 70, col: 39, offset: 1808},
						alternatives: []interface{}{
							&seqExpr{
								pos: position{line: 70, col: 40, offset: 1809},
								exprs: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 70, col: 40, offset: 1809},
										name: "NonWhitespaceLiteralSeparator",
									},
									&zeroOrOneExpr{
										pos: position{line: 70, col: 70, offset: 1839},
										expr: &ruleRefExpr{
											pos:  position{line: 70, col: 70, offset: 1839},
											name: "Comment",
										},
									},
								},
							},
							&seqExpr{
								pos: position{line: 70, col: 83, offset: 1852},
								exprs: []interface{}{
									&zeroOrOneExpr{
										pos: position{line: 70, col: 83, offset: 1852},
										expr: &ruleRefExpr{
											pos:  position{line: 70, col: 83, offset: 1852},
											name: "Comment",
										},
									},
									&charClassMatcher{
										pos:        position{line: 70, col: 92, offset: 1861},
										val:        "[\\r\\n]",
										chars:      []rune{'\r', '\n'},
										ignoreCase: false,
										inverted:   false,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "NonWhitespaceLiteralSeparator",
			pos:  position{line: 72, col: 1, offset: 1871},
			expr: &litMatcher{
				pos:        position{line: 72, col: 34, offset: 1904},
				val:        ";",
				ignoreCase: false,
			},
		},
		{
			name: "Literal",
			pos:  position{line: 74, col: 1, offset: 1909},
			expr: &actionExpr{
				pos: position{line: 74, col: 12, offset: 1920},
				run: (*parser).callonLiteral1,
				expr: &seqExpr{
					pos: position{line: 74, col: 12, offset: 1920},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 74, col: 12, offset: 1920},
							label: "negated",
							expr: &zeroOrOneExpr{
								pos: position{line: 74, col: 20, offset: 1928},
								expr: &ruleRefExpr{
									pos:  position{line: 74, col: 20, offset: 1928},
									name: "NotKeyword",
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 74, col: 32, offset: 1940},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 74, col: 38, offset: 1946},
								name: "LiteralExpr",
							},
						},
						&labeledExpr{
							pos:   position{line: 74, col: 50, offset: 1958},
							label: "with",
							expr: &zeroOrOneExpr{
								pos: position{line: 74, col: 55, offset: 1963},
								expr: &ruleRefExpr{
									pos:  position{line: 74, col: 55, offset: 1963},
									name: "WithKeywordList",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "LiteralExpr",
			pos:  position{line: 78, col: 1, offset: 2030},
			expr: &actionExpr{
				pos: position{line: 78, col: 16, offset: 2045},
				run: (*parser).callonLiteralExpr1,
				expr: &seqExpr{
					pos: position{line: 78, col: 16, offset: 2045},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 78, col: 16, offset: 2045},
							label: "lhs",
							expr: &ruleRefExpr{
								pos:  position{line: 78, col: 20, offset: 2049},
								name: "ExprTerm",
							},
						},
						&labeledExpr{
							pos:   position{line: 78, col: 29, offset: 2058},
							label: "rest",
							expr: &zeroOrOneExpr{
								pos: position{line: 78, col: 34, offset: 2063},
								expr: &seqExpr{
									pos: position{line: 78, col: 36, offset: 2065},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 78, col: 36, offset: 2065},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 78, col: 38, offset: 2067},
											name: "LiteralExprOperator",
										},
										&ruleRefExpr{
											pos:  position{line: 78, col: 58, offset: 2087},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 78, col: 60, offset: 2089},
											name: "ExprTerm",
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
			name: "LiteralExprOperator",
			pos:  position{line: 82, col: 1, offset: 2163},
			expr: &actionExpr{
				pos: position{line: 82, col: 24, offset: 2186},
				run: (*parser).callonLiteralExprOperator1,
				expr: &labeledExpr{
					pos:   position{line: 82, col: 24, offset: 2186},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 82, col: 30, offset: 2192},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 82, col: 30, offset: 2192},
								val:        ":=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 82, col: 37, offset: 2199},
								val:        "=",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "NotKeyword",
			pos:  position{line: 86, col: 1, offset: 2267},
			expr: &actionExpr{
				pos: position{line: 86, col: 15, offset: 2281},
				run: (*parser).callonNotKeyword1,
				expr: &labeledExpr{
					pos:   position{line: 86, col: 15, offset: 2281},
					label: "val",
					expr: &zeroOrOneExpr{
						pos: position{line: 86, col: 19, offset: 2285},
						expr: &seqExpr{
							pos: position{line: 86, col: 20, offset: 2286},
							exprs: []interface{}{
								&litMatcher{
									pos:        position{line: 86, col: 20, offset: 2286},
									val:        "not",
									ignoreCase: false,
								},
								&ruleRefExpr{
									pos:  position{line: 86, col: 26, offset: 2292},
									name: "ws",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "WithKeywordList",
			pos:  position{line: 90, col: 1, offset: 2329},
			expr: &actionExpr{
				pos: position{line: 90, col: 20, offset: 2348},
				run: (*parser).callonWithKeywordList1,
				expr: &seqExpr{
					pos: position{line: 90, col: 20, offset: 2348},
					exprs: []interface{}{
						&ruleRefExpr{
							pos:  position{line: 90, col: 20, offset: 2348},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 90, col: 23, offset: 2351},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 90, col: 28, offset: 2356},
								name: "WithKeyword",
							},
						},
						&labeledExpr{
							pos:   position{line: 90, col: 40, offset: 2368},
							label: "rest",
							expr: &zeroOrMoreExpr{
								pos: position{line: 90, col: 45, offset: 2373},
								expr: &seqExpr{
									pos: position{line: 90, col: 47, offset: 2375},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 90, col: 47, offset: 2375},
											name: "ws",
										},
										&ruleRefExpr{
											pos:  position{line: 90, col: 50, offset: 2378},
											name: "WithKeyword",
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
			name: "WithKeyword",
			pos:  position{line: 94, col: 1, offset: 2441},
			expr: &actionExpr{
				pos: position{line: 94, col: 16, offset: 2456},
				run: (*parser).callonWithKeyword1,
				expr: &seqExpr{
					pos: position{line: 94, col: 16, offset: 2456},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 94, col: 16, offset: 2456},
							val:        "with",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 94, col: 23, offset: 2463},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 94, col: 26, offset: 2466},
							label: "target",
							expr: &ruleRefExpr{
								pos:  position{line: 94, col: 33, offset: 2473},
								name: "ExprTerm",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 94, col: 42, offset: 2482},
							name: "ws",
						},
						&litMatcher{
							pos:        position{line: 94, col: 45, offset: 2485},
							val:        "as",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 94, col: 50, offset: 2490},
							name: "ws",
						},
						&labeledExpr{
							pos:   position{line: 94, col: 53, offset: 2493},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 94, col: 59, offset: 2499},
								name: "ExprTerm",
							},
						},
					},
				},
			},
		},
		{
			name: "ExprTerm",
			pos:  position{line: 98, col: 1, offset: 2575},
			expr: &actionExpr{
				pos: position{line: 98, col: 13, offset: 2587},
				run: (*parser).callonExprTerm1,
				expr: &seqExpr{
					pos: position{line: 98, col: 13, offset: 2587},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 98, col: 13, offset: 2587},
							label: "lhs",
							expr: &ruleRefExpr{
								pos:  position{line: 98, col: 17, offset: 2591},
								name: "RelationExpr",
							},
						},
						&labeledExpr{
							pos:   position{line: 98, col: 30, offset: 2604},
							label: "rest",
							expr: &zeroOrMoreExpr{
								pos: position{line: 98, col: 35, offset: 2609},
								expr: &seqExpr{
									pos: position{line: 98, col: 37, offset: 2611},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 98, col: 37, offset: 2611},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 98, col: 39, offset: 2613},
											name: "RelationOperator",
										},
										&ruleRefExpr{
											pos:  position{line: 98, col: 56, offset: 2630},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 98, col: 58, offset: 2632},
											name: "RelationExpr",
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
			name: "ExprTermPairList",
			pos:  position{line: 102, col: 1, offset: 2708},
			expr: &actionExpr{
				pos: position{line: 102, col: 21, offset: 2728},
				run: (*parser).callonExprTermPairList1,
				expr: &seqExpr{
					pos: position{line: 102, col: 21, offset: 2728},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 102, col: 21, offset: 2728},
							label: "head",
							expr: &zeroOrOneExpr{
								pos: position{line: 102, col: 26, offset: 2733},
								expr: &ruleRefExpr{
									pos:  position{line: 102, col: 26, offset: 2733},
									name: "ExprTermPair",
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 102, col: 40, offset: 2747},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 102, col: 45, offset: 2752},
								expr: &seqExpr{
									pos: position{line: 102, col: 47, offset: 2754},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 102, col: 47, offset: 2754},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 102, col: 49, offset: 2756},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 102, col: 53, offset: 2760},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 102, col: 55, offset: 2762},
											name: "ExprTermPair",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 102, col: 71, offset: 2778},
							name: "_",
						},
						&zeroOrOneExpr{
							pos: position{line: 102, col: 73, offset: 2780},
							expr: &litMatcher{
								pos:        position{line: 102, col: 73, offset: 2780},
								val:        ",",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "ExprTermList",
			pos:  position{line: 106, col: 1, offset: 2834},
			expr: &actionExpr{
				pos: position{line: 106, col: 17, offset: 2850},
				run: (*parser).callonExprTermList1,
				expr: &seqExpr{
					pos: position{line: 106, col: 17, offset: 2850},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 106, col: 17, offset: 2850},
							label: "head",
							expr: &zeroOrOneExpr{
								pos: position{line: 106, col: 22, offset: 2855},
								expr: &ruleRefExpr{
									pos:  position{line: 106, col: 22, offset: 2855},
									name: "ExprTerm",
								},
							},
						},
						&labeledExpr{
							pos:   position{line: 106, col: 32, offset: 2865},
							label: "tail",
							expr: &zeroOrMoreExpr{
								pos: position{line: 106, col: 37, offset: 2870},
								expr: &seqExpr{
									pos: position{line: 106, col: 39, offset: 2872},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 106, col: 39, offset: 2872},
											name: "_",
										},
										&litMatcher{
											pos:        position{line: 106, col: 41, offset: 2874},
											val:        ",",
											ignoreCase: false,
										},
										&ruleRefExpr{
											pos:  position{line: 106, col: 45, offset: 2878},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 106, col: 47, offset: 2880},
											name: "ExprTerm",
										},
									},
								},
							},
						},
						&ruleRefExpr{
							pos:  position{line: 106, col: 59, offset: 2892},
							name: "_",
						},
						&zeroOrOneExpr{
							pos: position{line: 106, col: 61, offset: 2894},
							expr: &litMatcher{
								pos:        position{line: 106, col: 61, offset: 2894},
								val:        ",",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "ExprTermPair",
			pos:  position{line: 110, col: 1, offset: 2945},
			expr: &actionExpr{
				pos: position{line: 110, col: 17, offset: 2961},
				run: (*parser).callonExprTermPair1,
				expr: &seqExpr{
					pos: position{line: 110, col: 17, offset: 2961},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 110, col: 17, offset: 2961},
							label: "key",
							expr: &ruleRefExpr{
								pos:  position{line: 110, col: 21, offset: 2965},
								name: "ExprTerm",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 110, col: 30, offset: 2974},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 110, col: 32, offset: 2976},
							val:        ":",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 110, col: 36, offset: 2980},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 110, col: 38, offset: 2982},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 110, col: 44, offset: 2988},
								name: "ExprTerm",
							},
						},
					},
				},
			},
		},
		{
			name: "RelationOperator",
			pos:  position{line: 114, col: 1, offset: 3042},
			expr: &actionExpr{
				pos: position{line: 114, col: 21, offset: 3062},
				run: (*parser).callonRelationOperator1,
				expr: &labeledExpr{
					pos:   position{line: 114, col: 21, offset: 3062},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 114, col: 26, offset: 3067},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 114, col: 26, offset: 3067},
								val:        "==",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 114, col: 33, offset: 3074},
								val:        "!=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 114, col: 40, offset: 3081},
								val:        "<=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 114, col: 47, offset: 3088},
								val:        ">=",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 114, col: 54, offset: 3095},
								val:        ">",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 114, col: 60, offset: 3101},
								val:        "<",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "RelationExpr",
			pos:  position{line: 118, col: 1, offset: 3168},
			expr: &actionExpr{
				pos: position{line: 118, col: 17, offset: 3184},
				run: (*parser).callonRelationExpr1,
				expr: &seqExpr{
					pos: position{line: 118, col: 17, offset: 3184},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 118, col: 17, offset: 3184},
							label: "lhs",
							expr: &ruleRefExpr{
								pos:  position{line: 118, col: 21, offset: 3188},
								name: "BitwiseOrExpr",
							},
						},
						&labeledExpr{
							pos:   position{line: 118, col: 35, offset: 3202},
							label: "rest",
							expr: &zeroOrMoreExpr{
								pos: position{line: 118, col: 40, offset: 3207},
								expr: &seqExpr{
									pos: position{line: 118, col: 42, offset: 3209},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 118, col: 42, offset: 3209},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 118, col: 44, offset: 3211},
											name: "BitwiseOrOperator",
										},
										&ruleRefExpr{
											pos:  position{line: 118, col: 62, offset: 3229},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 118, col: 64, offset: 3231},
											name: "BitwiseOrExpr",
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
			name: "BitwiseOrOperator",
			pos:  position{line: 122, col: 1, offset: 3307},
			expr: &actionExpr{
				pos: position{line: 122, col: 22, offset: 3328},
				run: (*parser).callonBitwiseOrOperator1,
				expr: &labeledExpr{
					pos:   position{line: 122, col: 22, offset: 3328},
					label: "val",
					expr: &litMatcher{
						pos:        position{line: 122, col: 26, offset: 3332},
						val:        "|",
						ignoreCase: false,
					},
				},
			},
		},
		{
			name: "BitwiseOrExpr",
			pos:  position{line: 126, col: 1, offset: 3398},
			expr: &actionExpr{
				pos: position{line: 126, col: 18, offset: 3415},
				run: (*parser).callonBitwiseOrExpr1,
				expr: &seqExpr{
					pos: position{line: 126, col: 18, offset: 3415},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 126, col: 18, offset: 3415},
							label: "lhs",
							expr: &ruleRefExpr{
								pos:  position{line: 126, col: 22, offset: 3419},
								name: "BitwiseAndExpr",
							},
						},
						&labeledExpr{
							pos:   position{line: 126, col: 37, offset: 3434},
							label: "rest",
							expr: &zeroOrMoreExpr{
								pos: position{line: 126, col: 42, offset: 3439},
								expr: &seqExpr{
									pos: position{line: 126, col: 44, offset: 3441},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 126, col: 44, offset: 3441},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 126, col: 46, offset: 3443},
											name: "BitwiseAndOperator",
										},
										&ruleRefExpr{
											pos:  position{line: 126, col: 65, offset: 3462},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 126, col: 67, offset: 3464},
											name: "BitwiseAndExpr",
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
			name: "BitwiseAndOperator",
			pos:  position{line: 130, col: 1, offset: 3541},
			expr: &actionExpr{
				pos: position{line: 130, col: 23, offset: 3563},
				run: (*parser).callonBitwiseAndOperator1,
				expr: &labeledExpr{
					pos:   position{line: 130, col: 23, offset: 3563},
					label: "val",
					expr: &litMatcher{
						pos:        position{line: 130, col: 27, offset: 3567},
						val:        "&",
						ignoreCase: false,
					},
				},
			},
		},
		{
			name: "BitwiseAndExpr",
			pos:  position{line: 134, col: 1, offset: 3633},
			expr: &actionExpr{
				pos: position{line: 134, col: 19, offset: 3651},
				run: (*parser).callonBitwiseAndExpr1,
				expr: &seqExpr{
					pos: position{line: 134, col: 19, offset: 3651},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 134, col: 19, offset: 3651},
							label: "lhs",
							expr: &ruleRefExpr{
								pos:  position{line: 134, col: 23, offset: 3655},
								name: "ArithExpr",
							},
						},
						&labeledExpr{
							pos:   position{line: 134, col: 33, offset: 3665},
							label: "rest",
							expr: &zeroOrMoreExpr{
								pos: position{line: 134, col: 38, offset: 3670},
								expr: &seqExpr{
									pos: position{line: 134, col: 40, offset: 3672},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 134, col: 40, offset: 3672},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 134, col: 42, offset: 3674},
											name: "ArithOperator",
										},
										&ruleRefExpr{
											pos:  position{line: 134, col: 56, offset: 3688},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 134, col: 58, offset: 3690},
											name: "ArithExpr",
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
			name: "ArithOperator",
			pos:  position{line: 138, col: 1, offset: 3762},
			expr: &actionExpr{
				pos: position{line: 138, col: 18, offset: 3779},
				run: (*parser).callonArithOperator1,
				expr: &labeledExpr{
					pos:   position{line: 138, col: 18, offset: 3779},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 138, col: 23, offset: 3784},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 138, col: 23, offset: 3784},
								val:        "+",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 138, col: 29, offset: 3790},
								val:        "-",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "ArithExpr",
			pos:  position{line: 142, col: 1, offset: 3857},
			expr: &actionExpr{
				pos: position{line: 142, col: 14, offset: 3870},
				run: (*parser).callonArithExpr1,
				expr: &seqExpr{
					pos: position{line: 142, col: 14, offset: 3870},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 142, col: 14, offset: 3870},
							label: "lhs",
							expr: &ruleRefExpr{
								pos:  position{line: 142, col: 18, offset: 3874},
								name: "FactorExpr",
							},
						},
						&labeledExpr{
							pos:   position{line: 142, col: 29, offset: 3885},
							label: "rest",
							expr: &zeroOrMoreExpr{
								pos: position{line: 142, col: 34, offset: 3890},
								expr: &seqExpr{
									pos: position{line: 142, col: 36, offset: 3892},
									exprs: []interface{}{
										&ruleRefExpr{
											pos:  position{line: 142, col: 36, offset: 3892},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 142, col: 38, offset: 3894},
											name: "FactorOperator",
										},
										&ruleRefExpr{
											pos:  position{line: 142, col: 53, offset: 3909},
											name: "_",
										},
										&ruleRefExpr{
											pos:  position{line: 142, col: 55, offset: 3911},
											name: "FactorExpr",
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
			name: "FactorOperator",
			pos:  position{line: 146, col: 1, offset: 3985},
			expr: &actionExpr{
				pos: position{line: 146, col: 19, offset: 4003},
				run: (*parser).callonFactorOperator1,
				expr: &labeledExpr{
					pos:   position{line: 146, col: 19, offset: 4003},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 146, col: 24, offset: 4008},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 146, col: 24, offset: 4008},
								val:        "*",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 146, col: 30, offset: 4014},
								val:        "/",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "FactorExpr",
			pos:  position{line: 150, col: 1, offset: 4080},
			expr: &choiceExpr{
				pos: position{line: 150, col: 15, offset: 4094},
				alternatives: []interface{}{
					&actionExpr{
						pos: position{line: 150, col: 15, offset: 4094},
						run: (*parser).callonFactorExpr2,
						expr: &seqExpr{
							pos: position{line: 150, col: 17, offset: 4096},
							exprs: []interface{}{
								&litMatcher{
									pos:        position{line: 150, col: 17, offset: 4096},
									val:        "(",
									ignoreCase: false,
								},
								&ruleRefExpr{
									pos:  position{line: 150, col: 21, offset: 4100},
									name: "_",
								},
								&labeledExpr{
									pos:   position{line: 150, col: 23, offset: 4102},
									label: "expr",
									expr: &ruleRefExpr{
										pos:  position{line: 150, col: 28, offset: 4107},
										name: "ExprTerm",
									},
								},
								&ruleRefExpr{
									pos:  position{line: 150, col: 37, offset: 4116},
									name: "_",
								},
								&litMatcher{
									pos:        position{line: 150, col: 39, offset: 4118},
									val:        ")",
									ignoreCase: false,
								},
							},
						},
					},
					&actionExpr{
						pos: position{line: 152, col: 5, offset: 4151},
						run: (*parser).callonFactorExpr10,
						expr: &labeledExpr{
							pos:   position{line: 152, col: 5, offset: 4151},
							label: "term",
							expr: &ruleRefExpr{
								pos:  position{line: 152, col: 10, offset: 4156},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "Call",
			pos:  position{line: 156, col: 1, offset: 4187},
			expr: &actionExpr{
				pos: position{line: 156, col: 9, offset: 4195},
				run: (*parser).callonCall1,
				expr: &seqExpr{
					pos: position{line: 156, col: 9, offset: 4195},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 156, col: 9, offset: 4195},
							label: "operator",
							expr: &choiceExpr{
								pos: position{line: 156, col: 19, offset: 4205},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 156, col: 19, offset: 4205},
										name: "Ref",
									},
									&ruleRefExpr{
										pos:  position{line: 156, col: 25, offset: 4211},
										name: "Var",
									},
								},
							},
						},
						&litMatcher{
							pos:        position{line: 156, col: 30, offset: 4216},
							val:        "(",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 156, col: 34, offset: 4220},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 156, col: 36, offset: 4222},
							label: "args",
							expr: &ruleRefExpr{
								pos:  position{line: 156, col: 41, offset: 4227},
								name: "ExprTermList",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 156, col: 54, offset: 4240},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 156, col: 56, offset: 4242},
							val:        ")",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Term",
			pos:  position{line: 160, col: 1, offset: 4307},
			expr: &actionExpr{
				pos: position{line: 160, col: 9, offset: 4315},
				run: (*parser).callonTerm1,
				expr: &labeledExpr{
					pos:   position{line: 160, col: 9, offset: 4315},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 160, col: 15, offset: 4321},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 160, col: 15, offset: 4321},
								name: "Comprehension",
							},
							&ruleRefExpr{
								pos:  position{line: 160, col: 31, offset: 4337},
								name: "Composite",
							},
							&ruleRefExpr{
								pos:  position{line: 160, col: 43, offset: 4349},
								name: "Scalar",
							},
							&ruleRefExpr{
								pos:  position{line: 160, col: 52, offset: 4358},
								name: "Call",
							},
							&ruleRefExpr{
								pos:  position{line: 160, col: 59, offset: 4365},
								name: "Ref",
							},
							&ruleRefExpr{
								pos:  position{line: 160, col: 65, offset: 4371},
								name: "Var",
							},
						},
					},
				},
			},
		},
		{
			name: "TermPair",
			pos:  position{line: 164, col: 1, offset: 4402},
			expr: &actionExpr{
				pos: position{line: 164, col: 13, offset: 4414},
				run: (*parser).callonTermPair1,
				expr: &seqExpr{
					pos: position{line: 164, col: 13, offset: 4414},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 164, col: 13, offset: 4414},
							label: "key",
							expr: &ruleRefExpr{
								pos:  position{line: 164, col: 17, offset: 4418},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 164, col: 22, offset: 4423},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 164, col: 24, offset: 4425},
							val:        ":",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 164, col: 28, offset: 4429},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 164, col: 30, offset: 4431},
							label: "value",
							expr: &ruleRefExpr{
								pos:  position{line: 164, col: 36, offset: 4437},
								name: "Term",
							},
						},
					},
				},
			},
		},
		{
			name: "Comprehension",
			pos:  position{line: 168, col: 1, offset: 4487},
			expr: &choiceExpr{
				pos: position{line: 168, col: 18, offset: 4504},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 168, col: 18, offset: 4504},
						name: "ArrayComprehension",
					},
					&ruleRefExpr{
						pos:  position{line: 168, col: 39, offset: 4525},
						name: "ObjectComprehension",
					},
					&ruleRefExpr{
						pos:  position{line: 168, col: 61, offset: 4547},
						name: "SetComprehension",
					},
				},
			},
		},
		{
			name: "ArrayComprehension",
			pos:  position{line: 170, col: 1, offset: 4565},
			expr: &actionExpr{
				pos: position{line: 170, col: 23, offset: 4587},
				run: (*parser).callonArrayComprehension1,
				expr: &seqExpr{
					pos: position{line: 170, col: 23, offset: 4587},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 170, col: 23, offset: 4587},
							val:        "[",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 170, col: 27, offset: 4591},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 170, col: 29, offset: 4593},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 170, col: 34, offset: 4598},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 170, col: 39, offset: 4603},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 170, col: 41, offset: 4605},
							val:        "|",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 170, col: 45, offset: 4609},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 170, col: 47, offset: 4611},
							label: "body",
							expr: &ruleRefExpr{
								pos:  position{line: 170, col: 52, offset: 4616},
								name: "WhitespaceBody",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 170, col: 67, offset: 4631},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 170, col: 69, offset: 4633},
							val:        "]",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "ObjectComprehension",
			pos:  position{line: 174, col: 1, offset: 4708},
			expr: &actionExpr{
				pos: position{line: 174, col: 24, offset: 4731},
				run: (*parser).callonObjectComprehension1,
				expr: &seqExpr{
					pos: position{line: 174, col: 24, offset: 4731},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 174, col: 24, offset: 4731},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 174, col: 28, offset: 4735},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 174, col: 30, offset: 4737},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 174, col: 35, offset: 4742},
								name: "TermPair",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 174, col: 45, offset: 4752},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 174, col: 47, offset: 4754},
							val:        "|",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 174, col: 51, offset: 4758},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 174, col: 53, offset: 4760},
							label: "body",
							expr: &ruleRefExpr{
								pos:  position{line: 174, col: 58, offset: 4765},
								name: "WhitespaceBody",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 174, col: 73, offset: 4780},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 174, col: 75, offset: 4782},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "SetComprehension",
			pos:  position{line: 178, col: 1, offset: 4858},
			expr: &actionExpr{
				pos: position{line: 178, col: 21, offset: 4878},
				run: (*parser).callonSetComprehension1,
				expr: &seqExpr{
					pos: position{line: 178, col: 21, offset: 4878},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 178, col: 21, offset: 4878},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 178, col: 25, offset: 4882},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 178, col: 27, offset: 4884},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 178, col: 32, offset: 4889},
								name: "Term",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 178, col: 37, offset: 4894},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 178, col: 39, offset: 4896},
							val:        "|",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 178, col: 43, offset: 4900},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 178, col: 45, offset: 4902},
							label: "body",
							expr: &ruleRefExpr{
								pos:  position{line: 178, col: 50, offset: 4907},
								name: "WhitespaceBody",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 178, col: 65, offset: 4922},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 178, col: 67, offset: 4924},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Composite",
			pos:  position{line: 182, col: 1, offset: 4997},
			expr: &choiceExpr{
				pos: position{line: 182, col: 14, offset: 5010},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 182, col: 14, offset: 5010},
						name: "Object",
					},
					&ruleRefExpr{
						pos:  position{line: 182, col: 23, offset: 5019},
						name: "Array",
					},
					&ruleRefExpr{
						pos:  position{line: 182, col: 31, offset: 5027},
						name: "Set",
					},
				},
			},
		},
		{
			name: "Scalar",
			pos:  position{line: 184, col: 1, offset: 5032},
			expr: &choiceExpr{
				pos: position{line: 184, col: 11, offset: 5042},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 184, col: 11, offset: 5042},
						name: "Number",
					},
					&ruleRefExpr{
						pos:  position{line: 184, col: 20, offset: 5051},
						name: "String",
					},
					&ruleRefExpr{
						pos:  position{line: 184, col: 29, offset: 5060},
						name: "Bool",
					},
					&ruleRefExpr{
						pos:  position{line: 184, col: 36, offset: 5067},
						name: "Null",
					},
				},
			},
		},
		{
			name: "Object",
			pos:  position{line: 186, col: 1, offset: 5073},
			expr: &actionExpr{
				pos: position{line: 186, col: 11, offset: 5083},
				run: (*parser).callonObject1,
				expr: &seqExpr{
					pos: position{line: 186, col: 11, offset: 5083},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 186, col: 11, offset: 5083},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 186, col: 15, offset: 5087},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 186, col: 17, offset: 5089},
							label: "list",
							expr: &ruleRefExpr{
								pos:  position{line: 186, col: 22, offset: 5094},
								name: "ExprTermPairList",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 186, col: 39, offset: 5111},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 186, col: 41, offset: 5113},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Array",
			pos:  position{line: 190, col: 1, offset: 5170},
			expr: &actionExpr{
				pos: position{line: 190, col: 10, offset: 5179},
				run: (*parser).callonArray1,
				expr: &seqExpr{
					pos: position{line: 190, col: 10, offset: 5179},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 190, col: 10, offset: 5179},
							val:        "[",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 190, col: 14, offset: 5183},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 190, col: 16, offset: 5185},
							label: "list",
							expr: &ruleRefExpr{
								pos:  position{line: 190, col: 21, offset: 5190},
								name: "ExprTermList",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 190, col: 34, offset: 5203},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 190, col: 36, offset: 5205},
							val:        "]",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Set",
			pos:  position{line: 194, col: 1, offset: 5261},
			expr: &choiceExpr{
				pos: position{line: 194, col: 8, offset: 5268},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 194, col: 8, offset: 5268},
						name: "SetEmpty",
					},
					&ruleRefExpr{
						pos:  position{line: 194, col: 19, offset: 5279},
						name: "SetNonEmpty",
					},
				},
			},
		},
		{
			name: "SetEmpty",
			pos:  position{line: 196, col: 1, offset: 5292},
			expr: &actionExpr{
				pos: position{line: 196, col: 13, offset: 5304},
				run: (*parser).callonSetEmpty1,
				expr: &seqExpr{
					pos: position{line: 196, col: 13, offset: 5304},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 196, col: 13, offset: 5304},
							val:        "set(",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 196, col: 20, offset: 5311},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 196, col: 22, offset: 5313},
							val:        ")",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "SetNonEmpty",
			pos:  position{line: 201, col: 1, offset: 5390},
			expr: &actionExpr{
				pos: position{line: 201, col: 16, offset: 5405},
				run: (*parser).callonSetNonEmpty1,
				expr: &seqExpr{
					pos: position{line: 201, col: 16, offset: 5405},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 201, col: 16, offset: 5405},
							val:        "{",
							ignoreCase: false,
						},
						&ruleRefExpr{
							pos:  position{line: 201, col: 20, offset: 5409},
							name: "_",
						},
						&labeledExpr{
							pos:   position{line: 201, col: 22, offset: 5411},
							label: "list",
							expr: &ruleRefExpr{
								pos:  position{line: 201, col: 27, offset: 5416},
								name: "ExprTermList",
							},
						},
						&ruleRefExpr{
							pos:  position{line: 201, col: 40, offset: 5429},
							name: "_",
						},
						&litMatcher{
							pos:        position{line: 201, col: 42, offset: 5431},
							val:        "}",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Ref",
			pos:  position{line: 205, col: 1, offset: 5485},
			expr: &actionExpr{
				pos: position{line: 205, col: 8, offset: 5492},
				run: (*parser).callonRef1,
				expr: &seqExpr{
					pos: position{line: 205, col: 8, offset: 5492},
					exprs: []interface{}{
						&labeledExpr{
							pos:   position{line: 205, col: 8, offset: 5492},
							label: "head",
							expr: &ruleRefExpr{
								pos:  position{line: 205, col: 13, offset: 5497},
								name: "Var",
							},
						},
						&labeledExpr{
							pos:   position{line: 205, col: 17, offset: 5501},
							label: "rest",
							expr: &oneOrMoreExpr{
								pos: position{line: 205, col: 22, offset: 5506},
								expr: &ruleRefExpr{
									pos:  position{line: 205, col: 22, offset: 5506},
									name: "RefOperand",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "RefOperand",
			pos:  position{line: 209, col: 1, offset: 5574},
			expr: &choiceExpr{
				pos: position{line: 209, col: 15, offset: 5588},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 209, col: 15, offset: 5588},
						name: "RefOperandDot",
					},
					&ruleRefExpr{
						pos:  position{line: 209, col: 31, offset: 5604},
						name: "RefOperandCanonical",
					},
				},
			},
		},
		{
			name: "RefOperandDot",
			pos:  position{line: 211, col: 1, offset: 5625},
			expr: &actionExpr{
				pos: position{line: 211, col: 18, offset: 5642},
				run: (*parser).callonRefOperandDot1,
				expr: &seqExpr{
					pos: position{line: 211, col: 18, offset: 5642},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 211, col: 18, offset: 5642},
							val:        ".",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 211, col: 22, offset: 5646},
							label: "val",
							expr: &ruleRefExpr{
								pos:  position{line: 211, col: 26, offset: 5650},
								name: "Var",
							},
						},
					},
				},
			},
		},
		{
			name: "RefOperandCanonical",
			pos:  position{line: 215, col: 1, offset: 5713},
			expr: &actionExpr{
				pos: position{line: 215, col: 24, offset: 5736},
				run: (*parser).callonRefOperandCanonical1,
				expr: &seqExpr{
					pos: position{line: 215, col: 24, offset: 5736},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 215, col: 24, offset: 5736},
							val:        "[",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 215, col: 28, offset: 5740},
							label: "val",
							expr: &ruleRefExpr{
								pos:  position{line: 215, col: 32, offset: 5744},
								name: "ExprTerm",
							},
						},
						&litMatcher{
							pos:        position{line: 215, col: 41, offset: 5753},
							val:        "]",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Var",
			pos:  position{line: 219, col: 1, offset: 5782},
			expr: &actionExpr{
				pos: position{line: 219, col: 8, offset: 5789},
				run: (*parser).callonVar1,
				expr: &labeledExpr{
					pos:   position{line: 219, col: 8, offset: 5789},
					label: "val",
					expr: &ruleRefExpr{
						pos:  position{line: 219, col: 12, offset: 5793},
						name: "VarChecked",
					},
				},
			},
		},
		{
			name: "VarChecked",
			pos:  position{line: 223, col: 1, offset: 5848},
			expr: &seqExpr{
				pos: position{line: 223, col: 15, offset: 5862},
				exprs: []interface{}{
					&labeledExpr{
						pos:   position{line: 223, col: 15, offset: 5862},
						label: "val",
						expr: &ruleRefExpr{
							pos:  position{line: 223, col: 19, offset: 5866},
							name: "VarUnchecked",
						},
					},
					&notCodeExpr{
						pos: position{line: 223, col: 32, offset: 5879},
						run: (*parser).callonVarChecked4,
					},
				},
			},
		},
		{
			name: "VarUnchecked",
			pos:  position{line: 227, col: 1, offset: 5944},
			expr: &actionExpr{
				pos: position{line: 227, col: 17, offset: 5960},
				run: (*parser).callonVarUnchecked1,
				expr: &seqExpr{
					pos: position{line: 227, col: 17, offset: 5960},
					exprs: []interface{}{
						&ruleRefExpr{
							pos:  position{line: 227, col: 17, offset: 5960},
							name: "AsciiLetter",
						},
						&zeroOrMoreExpr{
							pos: position{line: 227, col: 29, offset: 5972},
							expr: &choiceExpr{
								pos: position{line: 227, col: 30, offset: 5973},
								alternatives: []interface{}{
									&ruleRefExpr{
										pos:  position{line: 227, col: 30, offset: 5973},
										name: "AsciiLetter",
									},
									&ruleRefExpr{
										pos:  position{line: 227, col: 44, offset: 5987},
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
			pos:  position{line: 231, col: 1, offset: 6054},
			expr: &actionExpr{
				pos: position{line: 231, col: 11, offset: 6064},
				run: (*parser).callonNumber1,
				expr: &seqExpr{
					pos: position{line: 231, col: 11, offset: 6064},
					exprs: []interface{}{
						&zeroOrOneExpr{
							pos: position{line: 231, col: 11, offset: 6064},
							expr: &litMatcher{
								pos:        position{line: 231, col: 11, offset: 6064},
								val:        "-",
								ignoreCase: false,
							},
						},
						&choiceExpr{
							pos: position{line: 231, col: 18, offset: 6071},
							alternatives: []interface{}{
								&ruleRefExpr{
									pos:  position{line: 231, col: 18, offset: 6071},
									name: "Float",
								},
								&ruleRefExpr{
									pos:  position{line: 231, col: 26, offset: 6079},
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
			pos:  position{line: 235, col: 1, offset: 6144},
			expr: &choiceExpr{
				pos: position{line: 235, col: 10, offset: 6153},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 235, col: 10, offset: 6153},
						name: "ExponentFloat",
					},
					&ruleRefExpr{
						pos:  position{line: 235, col: 26, offset: 6169},
						name: "PointFloat",
					},
				},
			},
		},
		{
			name: "ExponentFloat",
			pos:  position{line: 237, col: 1, offset: 6181},
			expr: &seqExpr{
				pos: position{line: 237, col: 18, offset: 6198},
				exprs: []interface{}{
					&choiceExpr{
						pos: position{line: 237, col: 20, offset: 6200},
						alternatives: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 237, col: 20, offset: 6200},
								name: "PointFloat",
							},
							&ruleRefExpr{
								pos:  position{line: 237, col: 33, offset: 6213},
								name: "Integer",
							},
						},
					},
					&ruleRefExpr{
						pos:  position{line: 237, col: 43, offset: 6223},
						name: "Exponent",
					},
				},
			},
		},
		{
			name: "PointFloat",
			pos:  position{line: 239, col: 1, offset: 6233},
			expr: &seqExpr{
				pos: position{line: 239, col: 15, offset: 6247},
				exprs: []interface{}{
					&zeroOrOneExpr{
						pos: position{line: 239, col: 15, offset: 6247},
						expr: &ruleRefExpr{
							pos:  position{line: 239, col: 15, offset: 6247},
							name: "Integer",
						},
					},
					&ruleRefExpr{
						pos:  position{line: 239, col: 24, offset: 6256},
						name: "Fraction",
					},
				},
			},
		},
		{
			name: "Fraction",
			pos:  position{line: 241, col: 1, offset: 6266},
			expr: &seqExpr{
				pos: position{line: 241, col: 13, offset: 6278},
				exprs: []interface{}{
					&litMatcher{
						pos:        position{line: 241, col: 13, offset: 6278},
						val:        ".",
						ignoreCase: false,
					},
					&oneOrMoreExpr{
						pos: position{line: 241, col: 17, offset: 6282},
						expr: &ruleRefExpr{
							pos:  position{line: 241, col: 17, offset: 6282},
							name: "DecimalDigit",
						},
					},
				},
			},
		},
		{
			name: "Exponent",
			pos:  position{line: 243, col: 1, offset: 6297},
			expr: &seqExpr{
				pos: position{line: 243, col: 13, offset: 6309},
				exprs: []interface{}{
					&litMatcher{
						pos:        position{line: 243, col: 13, offset: 6309},
						val:        "e",
						ignoreCase: true,
					},
					&zeroOrOneExpr{
						pos: position{line: 243, col: 18, offset: 6314},
						expr: &charClassMatcher{
							pos:        position{line: 243, col: 18, offset: 6314},
							val:        "[+-]",
							chars:      []rune{'+', '-'},
							ignoreCase: false,
							inverted:   false,
						},
					},
					&oneOrMoreExpr{
						pos: position{line: 243, col: 24, offset: 6320},
						expr: &ruleRefExpr{
							pos:  position{line: 243, col: 24, offset: 6320},
							name: "DecimalDigit",
						},
					},
				},
			},
		},
		{
			name: "Integer",
			pos:  position{line: 245, col: 1, offset: 6335},
			expr: &choiceExpr{
				pos: position{line: 245, col: 12, offset: 6346},
				alternatives: []interface{}{
					&litMatcher{
						pos:        position{line: 245, col: 12, offset: 6346},
						val:        "0",
						ignoreCase: false,
					},
					&seqExpr{
						pos: position{line: 245, col: 20, offset: 6354},
						exprs: []interface{}{
							&ruleRefExpr{
								pos:  position{line: 245, col: 20, offset: 6354},
								name: "NonZeroDecimalDigit",
							},
							&zeroOrMoreExpr{
								pos: position{line: 245, col: 40, offset: 6374},
								expr: &ruleRefExpr{
									pos:  position{line: 245, col: 40, offset: 6374},
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
			pos:  position{line: 247, col: 1, offset: 6391},
			expr: &choiceExpr{
				pos: position{line: 247, col: 11, offset: 6401},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 247, col: 11, offset: 6401},
						name: "QuotedString",
					},
					&ruleRefExpr{
						pos:  position{line: 247, col: 26, offset: 6416},
						name: "RawString",
					},
				},
			},
		},
		{
			name: "QuotedString",
			pos:  position{line: 249, col: 1, offset: 6427},
			expr: &actionExpr{
				pos: position{line: 249, col: 17, offset: 6443},
				run: (*parser).callonQuotedString1,
				expr: &seqExpr{
					pos: position{line: 249, col: 17, offset: 6443},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 249, col: 17, offset: 6443},
							val:        "\"",
							ignoreCase: false,
						},
						&zeroOrMoreExpr{
							pos: position{line: 249, col: 21, offset: 6447},
							expr: &ruleRefExpr{
								pos:  position{line: 249, col: 21, offset: 6447},
								name: "Char",
							},
						},
						&litMatcher{
							pos:        position{line: 249, col: 27, offset: 6453},
							val:        "\"",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "RawString",
			pos:  position{line: 253, col: 1, offset: 6512},
			expr: &actionExpr{
				pos: position{line: 253, col: 14, offset: 6525},
				run: (*parser).callonRawString1,
				expr: &seqExpr{
					pos: position{line: 253, col: 14, offset: 6525},
					exprs: []interface{}{
						&litMatcher{
							pos:        position{line: 253, col: 14, offset: 6525},
							val:        "`",
							ignoreCase: false,
						},
						&zeroOrMoreExpr{
							pos: position{line: 253, col: 18, offset: 6529},
							expr: &charClassMatcher{
								pos:        position{line: 253, col: 18, offset: 6529},
								val:        "[^`]",
								chars:      []rune{'`'},
								ignoreCase: false,
								inverted:   true,
							},
						},
						&litMatcher{
							pos:        position{line: 253, col: 24, offset: 6535},
							val:        "`",
							ignoreCase: false,
						},
					},
				},
			},
		},
		{
			name: "Bool",
			pos:  position{line: 257, col: 1, offset: 6597},
			expr: &actionExpr{
				pos: position{line: 257, col: 9, offset: 6605},
				run: (*parser).callonBool1,
				expr: &labeledExpr{
					pos:   position{line: 257, col: 9, offset: 6605},
					label: "val",
					expr: &choiceExpr{
						pos: position{line: 257, col: 14, offset: 6610},
						alternatives: []interface{}{
							&litMatcher{
								pos:        position{line: 257, col: 14, offset: 6610},
								val:        "true",
								ignoreCase: false,
							},
							&litMatcher{
								pos:        position{line: 257, col: 23, offset: 6619},
								val:        "false",
								ignoreCase: false,
							},
						},
					},
				},
			},
		},
		{
			name: "Null",
			pos:  position{line: 261, col: 1, offset: 6681},
			expr: &actionExpr{
				pos: position{line: 261, col: 9, offset: 6689},
				run: (*parser).callonNull1,
				expr: &litMatcher{
					pos:        position{line: 261, col: 9, offset: 6689},
					val:        "null",
					ignoreCase: false,
				},
			},
		},
		{
			name: "AsciiLetter",
			pos:  position{line: 265, col: 1, offset: 6741},
			expr: &charClassMatcher{
				pos:        position{line: 265, col: 16, offset: 6756},
				val:        "[A-Za-z_]",
				chars:      []rune{'_'},
				ranges:     []rune{'A', 'Z', 'a', 'z'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "Char",
			pos:  position{line: 267, col: 1, offset: 6767},
			expr: &choiceExpr{
				pos: position{line: 267, col: 9, offset: 6775},
				alternatives: []interface{}{
					&seqExpr{
						pos: position{line: 267, col: 11, offset: 6777},
						exprs: []interface{}{
							&notExpr{
								pos: position{line: 267, col: 11, offset: 6777},
								expr: &ruleRefExpr{
									pos:  position{line: 267, col: 12, offset: 6778},
									name: "EscapedChar",
								},
							},
							&anyMatcher{
								line: 267, col: 24, offset: 6790,
							},
						},
					},
					&seqExpr{
						pos: position{line: 267, col: 32, offset: 6798},
						exprs: []interface{}{
							&litMatcher{
								pos:        position{line: 267, col: 32, offset: 6798},
								val:        "\\",
								ignoreCase: false,
							},
							&ruleRefExpr{
								pos:  position{line: 267, col: 37, offset: 6803},
								name: "EscapeSequence",
							},
						},
					},
				},
			},
		},
		{
			name: "EscapedChar",
			pos:  position{line: 269, col: 1, offset: 6821},
			expr: &charClassMatcher{
				pos:        position{line: 269, col: 16, offset: 6836},
				val:        "[\\x00-\\x1f\"\\\\]",
				chars:      []rune{'"', '\\'},
				ranges:     []rune{'\x00', '\x1f'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "EscapeSequence",
			pos:  position{line: 271, col: 1, offset: 6852},
			expr: &choiceExpr{
				pos: position{line: 271, col: 19, offset: 6870},
				alternatives: []interface{}{
					&ruleRefExpr{
						pos:  position{line: 271, col: 19, offset: 6870},
						name: "SingleCharEscape",
					},
					&ruleRefExpr{
						pos:  position{line: 271, col: 38, offset: 6889},
						name: "UnicodeEscape",
					},
				},
			},
		},
		{
			name: "SingleCharEscape",
			pos:  position{line: 273, col: 1, offset: 6904},
			expr: &charClassMatcher{
				pos:        position{line: 273, col: 21, offset: 6924},
				val:        "[ \" \\\\ / b f n r t ]",
				chars:      []rune{' ', '"', ' ', '\\', ' ', '/', ' ', 'b', ' ', 'f', ' ', 'n', ' ', 'r', ' ', 't', ' '},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "UnicodeEscape",
			pos:  position{line: 275, col: 1, offset: 6946},
			expr: &seqExpr{
				pos: position{line: 275, col: 18, offset: 6963},
				exprs: []interface{}{
					&litMatcher{
						pos:        position{line: 275, col: 18, offset: 6963},
						val:        "u",
						ignoreCase: false,
					},
					&ruleRefExpr{
						pos:  position{line: 275, col: 22, offset: 6967},
						name: "HexDigit",
					},
					&ruleRefExpr{
						pos:  position{line: 275, col: 31, offset: 6976},
						name: "HexDigit",
					},
					&ruleRefExpr{
						pos:  position{line: 275, col: 40, offset: 6985},
						name: "HexDigit",
					},
					&ruleRefExpr{
						pos:  position{line: 275, col: 49, offset: 6994},
						name: "HexDigit",
					},
				},
			},
		},
		{
			name: "DecimalDigit",
			pos:  position{line: 277, col: 1, offset: 7004},
			expr: &charClassMatcher{
				pos:        position{line: 277, col: 17, offset: 7020},
				val:        "[0-9]",
				ranges:     []rune{'0', '9'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "NonZeroDecimalDigit",
			pos:  position{line: 279, col: 1, offset: 7027},
			expr: &charClassMatcher{
				pos:        position{line: 279, col: 24, offset: 7050},
				val:        "[1-9]",
				ranges:     []rune{'1', '9'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name: "HexDigit",
			pos:  position{line: 281, col: 1, offset: 7057},
			expr: &charClassMatcher{
				pos:        position{line: 281, col: 13, offset: 7069},
				val:        "[0-9a-fA-F]",
				ranges:     []rune{'0', '9', 'a', 'f', 'A', 'F'},
				ignoreCase: false,
				inverted:   false,
			},
		},
		{
			name:        "ws",
			displayName: "\"whitespace\"",
			pos:         position{line: 283, col: 1, offset: 7082},
			expr: &oneOrMoreExpr{
				pos: position{line: 283, col: 20, offset: 7101},
				expr: &charClassMatcher{
					pos:        position{line: 283, col: 20, offset: 7101},
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
			pos:         position{line: 285, col: 1, offset: 7113},
			expr: &zeroOrMoreExpr{
				pos: position{line: 285, col: 19, offset: 7131},
				expr: &choiceExpr{
					pos: position{line: 285, col: 21, offset: 7133},
					alternatives: []interface{}{
						&charClassMatcher{
							pos:        position{line: 285, col: 21, offset: 7133},
							val:        "[ \\t\\r\\n]",
							chars:      []rune{' ', '\t', '\r', '\n'},
							ignoreCase: false,
							inverted:   false,
						},
						&ruleRefExpr{
							pos:  position{line: 285, col: 33, offset: 7145},
							name: "Comment",
						},
					},
				},
			},
		},
		{
			name: "Comment",
			pos:  position{line: 287, col: 1, offset: 7157},
			expr: &actionExpr{
				pos: position{line: 287, col: 12, offset: 7168},
				run: (*parser).callonComment1,
				expr: &seqExpr{
					pos: position{line: 287, col: 12, offset: 7168},
					exprs: []interface{}{
						&zeroOrMoreExpr{
							pos: position{line: 287, col: 12, offset: 7168},
							expr: &charClassMatcher{
								pos:        position{line: 287, col: 12, offset: 7168},
								val:        "[ \\t]",
								chars:      []rune{' ', '\t'},
								ignoreCase: false,
								inverted:   false,
							},
						},
						&litMatcher{
							pos:        position{line: 287, col: 19, offset: 7175},
							val:        "#",
							ignoreCase: false,
						},
						&labeledExpr{
							pos:   position{line: 287, col: 23, offset: 7179},
							label: "text",
							expr: &zeroOrMoreExpr{
								pos: position{line: 287, col: 28, offset: 7184},
								expr: &charClassMatcher{
									pos:        position{line: 287, col: 28, offset: 7184},
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
			pos:  position{line: 291, col: 1, offset: 7231},
			expr: &notExpr{
				pos: position{line: 291, col: 8, offset: 7238},
				expr: &anyMatcher{
					line: 291, col: 9, offset: 7239,
				},
			},
		},
	},
}

func (c *current) onProgram1(vals interface{}) (interface{}, error) {
	return makeProgram(c, vals)
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
	return makePackage(currentLocation(c), val)

}

func (p *parser) callonPackage1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onPackage1(stack["val"])
}

func (c *current) onImport1(path, alias interface{}) (interface{}, error) {
	return makeImport(currentLocation(c), path, alias)
}

func (p *parser) callonImport1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onImport1(stack["path"], stack["alias"])
}

func (c *current) onDefaultRules1(name, value interface{}) (interface{}, error) {
	return makeDefaultRule(currentLocation(c), name, value)
}

func (p *parser) callonDefaultRules1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onDefaultRules1(stack["name"], stack["value"])
}

func (c *current) onNormalRules1(head, rest interface{}) (interface{}, error) {
	return makeRule(currentLocation(c), head, rest)
}

func (p *parser) callonNormalRules1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNormalRules1(stack["head"], stack["rest"])
}

func (c *current) onRuleHead1(name, args, key, value interface{}) (interface{}, error) {
	return makeRuleHead(currentLocation(c), name, args, key, value)
}

func (p *parser) callonRuleHead1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRuleHead1(stack["name"], stack["args"], stack["key"], stack["value"])
}

func (c *current) onArgs1(list interface{}) (interface{}, error) {
	return makeArgs(list)
}

func (p *parser) callonArgs1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArgs1(stack["list"])
}

func (c *current) onElse1(value, body interface{}) (interface{}, error) {
	return makeRuleExt(currentLocation(c), value, body)
}

func (p *parser) callonElse1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onElse1(stack["value"], stack["body"])
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
	return makeBraceEnclosedBody(currentLocation(c), val)
}

func (p *parser) callonBraceEnclosedBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBraceEnclosedBody1(stack["val"])
}

func (c *current) onWhitespaceBody1(head, tail interface{}) (interface{}, error) {
	return makeBody(head, tail, 2)
}

func (p *parser) callonWhitespaceBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onWhitespaceBody1(stack["head"], stack["tail"])
}

func (c *current) onNonWhitespaceBody1(head, tail interface{}) (interface{}, error) {
	return makeBody(head, tail, 3)
}

func (p *parser) callonNonWhitespaceBody1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNonWhitespaceBody1(stack["head"], stack["tail"])
}

func (c *current) onLiteral1(negated, value, with interface{}) (interface{}, error) {
	return makeLiteral(negated, value, with)
}

func (p *parser) callonLiteral1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onLiteral1(stack["negated"], stack["value"], stack["with"])
}

func (c *current) onLiteralExpr1(lhs, rest interface{}) (interface{}, error) {
	return makeLiteralExpr(currentLocation(c), lhs, rest)
}

func (p *parser) callonLiteralExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onLiteralExpr1(stack["lhs"], stack["rest"])
}

func (c *current) onLiteralExprOperator1(val interface{}) (interface{}, error) {
	return makeInfixOperator(currentLocation(c), c.text)
}

func (p *parser) callonLiteralExprOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onLiteralExprOperator1(stack["val"])
}

func (c *current) onNotKeyword1(val interface{}) (interface{}, error) {
	return val != nil, nil
}

func (p *parser) callonNotKeyword1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNotKeyword1(stack["val"])
}

func (c *current) onWithKeywordList1(head, rest interface{}) (interface{}, error) {
	return makeWithKeywordList(head, rest)
}

func (p *parser) callonWithKeywordList1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onWithKeywordList1(stack["head"], stack["rest"])
}

func (c *current) onWithKeyword1(target, value interface{}) (interface{}, error) {
	return makeWithKeyword(currentLocation(c), target, value)
}

func (p *parser) callonWithKeyword1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onWithKeyword1(stack["target"], stack["value"])
}

func (c *current) onExprTerm1(lhs, rest interface{}) (interface{}, error) {
	return makeExprTerm(currentLocation(c), lhs, rest)
}

func (p *parser) callonExprTerm1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onExprTerm1(stack["lhs"], stack["rest"])
}

func (c *current) onExprTermPairList1(head, tail interface{}) (interface{}, error) {
	return makeExprTermPairList(head, tail)
}

func (p *parser) callonExprTermPairList1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onExprTermPairList1(stack["head"], stack["tail"])
}

func (c *current) onExprTermList1(head, tail interface{}) (interface{}, error) {
	return makeExprTermList(head, tail)
}

func (p *parser) callonExprTermList1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onExprTermList1(stack["head"], stack["tail"])
}

func (c *current) onExprTermPair1(key, value interface{}) (interface{}, error) {
	return makeExprTermPair(key, value)
}

func (p *parser) callonExprTermPair1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onExprTermPair1(stack["key"], stack["value"])
}

func (c *current) onRelationOperator1(val interface{}) (interface{}, error) {
	return makeInfixOperator(currentLocation(c), c.text)
}

func (p *parser) callonRelationOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRelationOperator1(stack["val"])
}

func (c *current) onRelationExpr1(lhs, rest interface{}) (interface{}, error) {
	return makeExprTerm(currentLocation(c), lhs, rest)
}

func (p *parser) callonRelationExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRelationExpr1(stack["lhs"], stack["rest"])
}

func (c *current) onBitwiseOrOperator1(val interface{}) (interface{}, error) {
	return makeInfixOperator(currentLocation(c), c.text)
}

func (p *parser) callonBitwiseOrOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBitwiseOrOperator1(stack["val"])
}

func (c *current) onBitwiseOrExpr1(lhs, rest interface{}) (interface{}, error) {
	return makeExprTerm(currentLocation(c), lhs, rest)
}

func (p *parser) callonBitwiseOrExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBitwiseOrExpr1(stack["lhs"], stack["rest"])
}

func (c *current) onBitwiseAndOperator1(val interface{}) (interface{}, error) {
	return makeInfixOperator(currentLocation(c), c.text)
}

func (p *parser) callonBitwiseAndOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBitwiseAndOperator1(stack["val"])
}

func (c *current) onBitwiseAndExpr1(lhs, rest interface{}) (interface{}, error) {
	return makeExprTerm(currentLocation(c), lhs, rest)
}

func (p *parser) callonBitwiseAndExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBitwiseAndExpr1(stack["lhs"], stack["rest"])
}

func (c *current) onArithOperator1(val interface{}) (interface{}, error) {
	return makeInfixOperator(currentLocation(c), c.text)
}

func (p *parser) callonArithOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArithOperator1(stack["val"])
}

func (c *current) onArithExpr1(lhs, rest interface{}) (interface{}, error) {
	return makeExprTerm(currentLocation(c), lhs, rest)
}

func (p *parser) callonArithExpr1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArithExpr1(stack["lhs"], stack["rest"])
}

func (c *current) onFactorOperator1(val interface{}) (interface{}, error) {
	return makeInfixOperator(currentLocation(c), c.text)
}

func (p *parser) callonFactorOperator1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onFactorOperator1(stack["val"])
}

func (c *current) onFactorExpr2(expr interface{}) (interface{}, error) {
	return expr, nil
}

func (p *parser) callonFactorExpr2() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onFactorExpr2(stack["expr"])
}

func (c *current) onFactorExpr10(term interface{}) (interface{}, error) {
	return term, nil
}

func (p *parser) callonFactorExpr10() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onFactorExpr10(stack["term"])
}

func (c *current) onCall1(operator, args interface{}) (interface{}, error) {
	return makeCall(currentLocation(c), operator, args)
}

func (p *parser) callonCall1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onCall1(stack["operator"], stack["args"])
}

func (c *current) onTerm1(val interface{}) (interface{}, error) {
	return val, nil
}

func (p *parser) callonTerm1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onTerm1(stack["val"])
}

func (c *current) onTermPair1(key, value interface{}) (interface{}, error) {
	return makeExprTermPair(key, value)
}

func (p *parser) callonTermPair1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onTermPair1(stack["key"], stack["value"])
}

func (c *current) onArrayComprehension1(head, body interface{}) (interface{}, error) {
	return makeArrayComprehension(currentLocation(c), head, body)
}

func (p *parser) callonArrayComprehension1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArrayComprehension1(stack["head"], stack["body"])
}

func (c *current) onObjectComprehension1(head, body interface{}) (interface{}, error) {
	return makeObjectComprehension(currentLocation(c), head, body)
}

func (p *parser) callonObjectComprehension1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onObjectComprehension1(stack["head"], stack["body"])
}

func (c *current) onSetComprehension1(head, body interface{}) (interface{}, error) {
	return makeSetComprehension(currentLocation(c), head, body)
}

func (p *parser) callonSetComprehension1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onSetComprehension1(stack["head"], stack["body"])
}

func (c *current) onObject1(list interface{}) (interface{}, error) {
	return makeObject(currentLocation(c), list)
}

func (p *parser) callonObject1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onObject1(stack["list"])
}

func (c *current) onArray1(list interface{}) (interface{}, error) {
	return makeArray(currentLocation(c), list)
}

func (p *parser) callonArray1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onArray1(stack["list"])
}

func (c *current) onSetEmpty1() (interface{}, error) {
	var empty []*Term
	return makeSet(currentLocation(c), empty)
}

func (p *parser) callonSetEmpty1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onSetEmpty1()
}

func (c *current) onSetNonEmpty1(list interface{}) (interface{}, error) {
	return makeSet(currentLocation(c), list)
}

func (p *parser) callonSetNonEmpty1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onSetNonEmpty1(stack["list"])
}

func (c *current) onRef1(head, rest interface{}) (interface{}, error) {
	return makeRef(currentLocation(c), head, rest)
}

func (p *parser) callonRef1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRef1(stack["head"], stack["rest"])
}

func (c *current) onRefOperandDot1(val interface{}) (interface{}, error) {
	return makeRefOperandDot(currentLocation(c), val)
}

func (p *parser) callonRefOperandDot1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRefOperandDot1(stack["val"])
}

func (c *current) onRefOperandCanonical1(val interface{}) (interface{}, error) {
	return val, nil
}

func (p *parser) callonRefOperandCanonical1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRefOperandCanonical1(stack["val"])
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
	return makeVar(currentLocation(c), c.text)
}

func (p *parser) callonVarUnchecked1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onVarUnchecked1()
}

func (c *current) onNumber1() (interface{}, error) {
	return makeNumber(currentLocation(c), c.text)
}

func (p *parser) callonNumber1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNumber1()
}

func (c *current) onQuotedString1() (interface{}, error) {
	return makeString(currentLocation(c), c.text)
}

func (p *parser) callonQuotedString1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onQuotedString1()
}

func (c *current) onRawString1() (interface{}, error) {
	return makeRawString(currentLocation(c), c.text)
}

func (p *parser) callonRawString1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onRawString1()
}

func (c *current) onBool1(val interface{}) (interface{}, error) {
	return makeBool(currentLocation(c), c.text)
}

func (p *parser) callonBool1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onBool1(stack["val"])
}

func (c *current) onNull1() (interface{}, error) {
	return makeNull(currentLocation(c))
}

func (p *parser) callonNull1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onNull1()
}

func (c *current) onComment1(text interface{}) (interface{}, error) {
	return makeComments(c, text)
}

func (p *parser) callonComment1() (interface{}, error) {
	stack := p.vstack[len(p.vstack)-1]
	_ = stack
	return p.cur.onComment1(stack["text"])
}

var (
	// errNoRule is returned when the grammar to parse has no rule.
	errNoRule = errors.New("grammar has no rule")

	// errInvalidEntrypoint is returned when the specified entrypoint rule
	// does not exit.
	errInvalidEntrypoint = errors.New("invalid entrypoint")

	// errInvalidEncoding is returned when the source is not properly
	// utf8-encoded.
	errInvalidEncoding = errors.New("invalid encoding")

	// errMaxExprCnt is used to signal that the maximum number of
	// expressions have been parsed.
	errMaxExprCnt = errors.New("max number of expresssions parsed")
)

// Option is a function that can set an option on the parser. It returns
// the previous setting as an Option.
type Option func(*parser) Option

// MaxExpressions creates an Option to stop parsing after the provided
// number of expressions have been parsed, if the value is 0 then the parser will
// parse for as many steps as needed (possibly an infinite number).
//
// The default for maxExprCnt is 0.
func MaxExpressions(maxExprCnt uint64) Option {
	return func(p *parser) Option {
		oldMaxExprCnt := p.maxExprCnt
		p.maxExprCnt = maxExprCnt
		return MaxExpressions(oldMaxExprCnt)
	}
}

// Entrypoint creates an Option to set the rule name to use as entrypoint.
// The rule name must have been specified in the -alternate-entrypoints
// if generating the parser with the -optimize-grammar flag, otherwise
// it may have been optimized out. Passing an empty string sets the
// entrypoint to the first rule in the grammar.
//
// The default is to start parsing at the first rule in the grammar.
func Entrypoint(ruleName string) Option {
	return func(p *parser) Option {
		oldEntrypoint := p.entrypoint
		p.entrypoint = ruleName
		if ruleName == "" {
			p.entrypoint = g.rules[0].name
		}
		return Entrypoint(oldEntrypoint)
	}
}

// Statistics adds a user provided Stats struct to the parser to allow
// the user to process the results after the parsing has finished.
// Also the key for the "no match" counter is set.
//
// Example usage:
//
//     input := "input"
//     stats := Stats{}
//     _, err := Parse("input-file", []byte(input), Statistics(&stats, "no match"))
//     if err != nil {
//         log.Panicln(err)
//     }
//     b, err := json.MarshalIndent(stats.ChoiceAltCnt, "", "  ")
//     if err != nil {
//         log.Panicln(err)
//     }
//     fmt.Println(string(b))
//
func Statistics(stats *Stats, choiceNoMatch string) Option {
	return func(p *parser) Option {
		oldStats := p.Stats
		p.Stats = stats
		oldChoiceNoMatch := p.choiceNoMatch
		p.choiceNoMatch = choiceNoMatch
		if p.Stats.ChoiceAltCnt == nil {
			p.Stats.ChoiceAltCnt = make(map[string]map[string]int)
		}
		return Statistics(oldStats, oldChoiceNoMatch)
	}
}

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

// AllowInvalidUTF8 creates an Option to allow invalid UTF-8 bytes.
// Every invalid UTF-8 byte is treated as a utf8.RuneError (U+FFFD)
// by character class matchers and is matched by the any matcher.
// The returned matched value, c.text and c.offset are NOT affected.
//
// The default is false.
func AllowInvalidUTF8(b bool) Option {
	return func(p *parser) Option {
		old := p.allowInvalidUTF8
		p.allowInvalidUTF8 = b
		return AllowInvalidUTF8(old)
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

// InitState creates an Option to set a key to a certain value in
// the global "state" store.
func InitState(key string, value interface{}) Option {
	return func(p *parser) Option {
		old := p.cur.state[key]
		p.cur.state[key] = value
		return InitState(key, old)
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

	// state is a store for arbitrary key,value pairs that the user wants to be
	// tied to the backtracking of the parser.
	// This is always rolled back if a parsing rule fails.
	state storeDict

	// globalStore is a general store for the user to store arbitrary key-value
	// pairs that they need to manage and that they do not want tied to the
	// backtracking of the parser. This is only modified by the user and never
	// rolled back by the parser. It is always up to the user to keep this in a
	// consistent state.
	globalStore storeDict
}

type storeDict map[string]interface{}

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

type recoveryExpr struct {
	pos          position
	expr         interface{}
	recoverExpr  interface{}
	failureLabel []string
}

type seqExpr struct {
	pos   position
	exprs []interface{}
}

type throwExpr struct {
	pos   position
	label string
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

type stateCodeExpr struct {
	pos position
	run func(*parser) error
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
	stats := Stats{
		ChoiceAltCnt: make(map[string]map[string]int),
	}

	p := &parser{
		filename: filename,
		errs:     new(errList),
		data:     b,
		pt:       savepoint{position: position{line: 1}},
		recover:  true,
		cur: current{
			state:       make(storeDict),
			globalStore: make(storeDict),
		},
		maxFailPos:      position{col: 1, line: 1},
		maxFailExpected: make([]string, 0, 20),
		Stats:           &stats,
		// start rule is rule [0] unless an alternate entrypoint is specified
		entrypoint: g.rules[0].name,
		emptyState: make(storeDict),
	}
	p.setOptions(opts)

	if p.maxExprCnt == 0 {
		p.maxExprCnt = math.MaxUint64
	}

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

const choiceNoMatch = -1

// Stats stores some statistics, gathered during parsing
type Stats struct {
	// ExprCnt counts the number of expressions processed during parsing
	// This value is compared to the maximum number of expressions allowed
	// (set by the MaxExpressions option).
	ExprCnt uint64

	// ChoiceAltCnt is used to count for each ordered choice expression,
	// which alternative is used how may times.
	// These numbers allow to optimize the order of the ordered choice expression
	// to increase the performance of the parser
	//
	// The outer key of ChoiceAltCnt is composed of the name of the rule as well
	// as the line and the column of the ordered choice.
	// The inner key of ChoiceAltCnt is the number (one-based) of the matching alternative.
	// For each alternative the number of matches are counted. If an ordered choice does not
	// match, a special counter is incremented. The name of this counter is set with
	// the parser option Statistics.
	// For an alternative to be included in ChoiceAltCnt, it has to match at least once.
	ChoiceAltCnt map[string]map[string]int
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

	// parse fail
	maxFailPos            position
	maxFailExpected       []string
	maxFailInvertExpected bool

	// max number of expressions to be parsed
	maxExprCnt uint64
	// entrypoint for the parser
	entrypoint string

	allowInvalidUTF8 bool

	*Stats

	choiceNoMatch string
	// recovery expression stack, keeps track of the currently available recovery expression, these are traversed in reverse
	recoveryStack []map[string]interface{}

	// emptyState contains an empty storeDict, which is used to optimize cloneState if global "state" store is not used.
	emptyState storeDict
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

// push a recovery expression with its labels to the recoveryStack
func (p *parser) pushRecovery(labels []string, expr interface{}) {
	if cap(p.recoveryStack) == len(p.recoveryStack) {
		// create new empty slot in the stack
		p.recoveryStack = append(p.recoveryStack, nil)
	} else {
		// slice to 1 more
		p.recoveryStack = p.recoveryStack[:len(p.recoveryStack)+1]
	}

	m := make(map[string]interface{}, len(labels))
	for _, fl := range labels {
		m[fl] = expr
	}
	p.recoveryStack[len(p.recoveryStack)-1] = m
}

// pop a recovery expression from the recoveryStack
func (p *parser) popRecovery() {
	// GC that map
	p.recoveryStack[len(p.recoveryStack)-1] = nil

	p.recoveryStack = p.recoveryStack[:len(p.recoveryStack)-1]
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

	if rn == utf8.RuneError && n == 1 { // see utf8.DecodeRune
		if !p.allowInvalidUTF8 {
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

// Cloner is implemented by any value that has a Clone method, which returns a
// copy of the value. This is mainly used for types which are not passed by
// value (e.g map, slice, chan) or structs that contain such types.
//
// This is used in conjunction with the global state feature to create proper
// copies of the state to allow the parser to properly restore the state in
// the case of backtracking.
type Cloner interface {
	Clone() interface{}
}

// clone and return parser current state.
func (p *parser) cloneState() storeDict {
	if p.debug {
		defer p.out(p.in("cloneState"))
	}

	if len(p.cur.state) == 0 {
		if len(p.emptyState) > 0 {
			p.emptyState = make(storeDict)
		}
		return p.emptyState
	}

	state := make(storeDict, len(p.cur.state))
	for k, v := range p.cur.state {
		if c, ok := v.(Cloner); ok {
			state[k] = c.Clone()
		} else {
			state[k] = v
		}
	}
	return state
}

// restore parser current state to the state storeDict.
// every restoreState should applied only one time for every cloned state
func (p *parser) restoreState(state storeDict) {
	if p.debug {
		defer p.out(p.in("restoreState"))
	}
	p.cur.state = state
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

	startRule, ok := p.rules[p.entrypoint]
	if !ok {
		p.addErr(errInvalidEntrypoint)
		return nil, p.errs.err()
	}

	p.read() // advance to first rune
	val, ok = p.parseRule(startRule)
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

	p.ExprCnt++
	if p.ExprCnt > p.maxExprCnt {
		panic(errMaxExprCnt)
	}

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
	case *recoveryExpr:
		val, ok = p.parseRecoveryExpr(expr)
	case *ruleRefExpr:
		val, ok = p.parseRuleRefExpr(expr)
	case *seqExpr:
		val, ok = p.parseSeqExpr(expr)
	case *stateCodeExpr:
		val, ok = p.parseStateCodeExpr(expr)
	case *throwExpr:
		val, ok = p.parseThrowExpr(expr)
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
		state := p.cloneState()
		actVal, err := act.run(p)
		if err != nil {
			p.addErrAt(err, start.position, []string{})
		}
		p.restoreState(state)

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

	state := p.cloneState()

	ok, err := and.run(p)
	if err != nil {
		p.addErr(err)
	}
	p.restoreState(state)

	return nil, ok
}

func (p *parser) parseAndExpr(and *andExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseAndExpr"))
	}

	pt := p.pt
	state := p.cloneState()
	p.pushV()
	_, ok := p.parseExpr(and.expr)
	p.popV()
	p.restoreState(state)
	p.restore(pt)

	return nil, ok
}

func (p *parser) parseAnyMatcher(any *anyMatcher) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseAnyMatcher"))
	}

	if p.pt.rn == utf8.RuneError && p.pt.w == 0 {
		// EOF - see utf8.DecodeRune
		p.failAt(false, p.pt.position, ".")
		return nil, false
	}
	start := p.pt
	p.read()
	p.failAt(true, start.position, ".")
	return p.sliceFrom(start), true
}

func (p *parser) parseCharClassMatcher(chr *charClassMatcher) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseCharClassMatcher"))
	}

	cur := p.pt.rn
	start := p.pt

	// can't match EOF
	if cur == utf8.RuneError && p.pt.w == 0 { // see utf8.DecodeRune
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

func (p *parser) incChoiceAltCnt(ch *choiceExpr, altI int) {
	choiceIdent := fmt.Sprintf("%s %d:%d", p.rstack[len(p.rstack)-1].name, ch.pos.line, ch.pos.col)
	m := p.ChoiceAltCnt[choiceIdent]
	if m == nil {
		m = make(map[string]int)
		p.ChoiceAltCnt[choiceIdent] = m
	}
	// We increment altI by 1, so the keys do not start at 0
	alt := strconv.Itoa(altI + 1)
	if altI == choiceNoMatch {
		alt = p.choiceNoMatch
	}
	m[alt]++
}

func (p *parser) parseChoiceExpr(ch *choiceExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseChoiceExpr"))
	}

	for altI, alt := range ch.alternatives {
		// dummy assignment to prevent compile error if optimized
		_ = altI

		state := p.cloneState()

		p.pushV()
		val, ok := p.parseExpr(alt)
		p.popV()
		if ok {
			p.incChoiceAltCnt(ch, altI)
			return val, ok
		}
		p.restoreState(state)
	}
	p.incChoiceAltCnt(ch, choiceNoMatch)
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

	state := p.cloneState()

	ok, err := not.run(p)
	if err != nil {
		p.addErr(err)
	}
	p.restoreState(state)

	return nil, !ok
}

func (p *parser) parseNotExpr(not *notExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseNotExpr"))
	}

	pt := p.pt
	state := p.cloneState()
	p.pushV()
	p.maxFailInvertExpected = !p.maxFailInvertExpected
	_, ok := p.parseExpr(not.expr)
	p.maxFailInvertExpected = !p.maxFailInvertExpected
	p.popV()
	p.restoreState(state)
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

func (p *parser) parseRecoveryExpr(recover *recoveryExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseRecoveryExpr (" + strings.Join(recover.failureLabel, ",") + ")"))
	}

	p.pushRecovery(recover.failureLabel, recover.recoverExpr)
	val, ok := p.parseExpr(recover.expr)
	p.popRecovery()

	return val, ok
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
	state := p.cloneState()
	for _, expr := range seq.exprs {
		val, ok := p.parseExpr(expr)
		if !ok {
			p.restoreState(state)
			p.restore(pt)
			return nil, false
		}
		vals = append(vals, val)
	}
	return vals, true
}

func (p *parser) parseStateCodeExpr(state *stateCodeExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseStateCodeExpr"))
	}

	err := state.run(p)
	if err != nil {
		p.addErr(err)
	}
	return nil, true
}

func (p *parser) parseThrowExpr(expr *throwExpr) (interface{}, bool) {
	if p.debug {
		defer p.out(p.in("parseThrowExpr"))
	}

	for i := len(p.recoveryStack) - 1; i >= 0; i-- {
		if recoverExpr, ok := p.recoveryStack[i][expr.label]; ok {
			if val, ok := p.parseExpr(recoverExpr); ok {
				return val, ok
			}
		}
	}

	return nil, false
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
