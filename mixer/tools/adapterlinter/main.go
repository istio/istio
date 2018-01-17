package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

var exitCode int

var invalidImportPaths = map[string]string{
	"log": "\"log\" import is not recommended; Adapters must instead use env.Logger for logging during execution. " +
		"This logger understands about which adapter is running and routes the data to the place where the operator " +
		"wants to see it.",
	"github.com/golang/glog": "\"github.com/golang/glog\" import is not recommended; Adapters must instead use env.Logger for logging during execution. " +
		"This logger understands about which adapter is running and routes the data to the place where the operator " +
		"wants to see it.",
}

func main() {
	flag.Parse()
	var reports []string
	if flag.NArg() == 0 {
		reports = doAllDirs([]string{"."})
	} else {
		reports = doAllDirs(flag.Args())
	}

	for _, r := range reports {
		reportErr(r)
	}
	os.Exit(exitCode)
}

func doAllDirs(args []string) []string {
	reports := make([]string, 0)
	for _, name := range args {
		// Is it a directory?
		if fi, err := os.Stat(name); err == nil && fi.IsDir() {
			for _, r := range doDir(name) {
				reports = append(reports, r.msg)
			}
		} else {
			reportErr(fmt.Sprintf("not a directory: %s", name))
		}
	}
	return reports
}

func doDir(name string) Reports {
	notests := func(info os.FileInfo) bool {
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") &&
			!strings.HasSuffix(info.Name(), "_test.go") {
			return true
		}
		return false
	}
	fs := token.NewFileSet()
	pkgs, err := parser.ParseDir(fs, name, notests, parser.Mode(0))
	if err != nil {
		reportErr(fmt.Sprintf("%v", err))
		return nil
	}
	reports := make(Reports, 0)
	for _, pkg := range pkgs {
		for _, r := range doPackage(fs, pkg) {
			reports = append(reports, r)
		}
	}
	sort.Sort(reports)
	return reports
}

func doPackage(fs *token.FileSet, pkg *ast.Package) Reports {
	v := newVisitor(fs)
	for _, file := range pkg.Files {
		ast.Walk(&v, file)
	}
	return v.reports
}

func newVisitor(fs *token.FileSet) visitor {
	return visitor{
		fs: fs,
	}
}

type visitor struct {
	reports Reports
	fs      *token.FileSet
}

func (v *visitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	switch d := node.(type) {
	case *ast.GoStmt:
		v.reports = append(v.reports,
			Report{
				d.Pos(),
				fmt.Sprintf("%v:%v:%v:Adapters must use env.ScheduleWork or env.ScheduleDaemon in order to "+
					"dispatch goroutines. This ensures all adapter goroutines are prevented from crashing Mixer as a "+
					"whole by catching any panics they produce.",
					v.fs.Position(d.Pos()).Filename, v.fs.Position(d.Pos()).Line, v.fs.Position(d.Pos()).Column),
			})
	case *ast.ImportSpec:
		if d.Path != nil {
			p := strings.Trim(d.Path.Value, "\"")
			for badImp, reportMsg := range invalidImportPaths {
				if p == badImp {
					v.reports = append(v.reports,
						Report{
							d.Path.Pos(),
							fmt.Sprintf("%v:%v:%v:%s",
								v.fs.Position(d.Path.Pos()).Filename,
								v.fs.Position(d.Path.Pos()).Line,
								v.fs.Position(d.Path.Pos()).Column,
								reportMsg),
						})
				}
			}
		}
	}

	return v
}

type Report struct {
	pos token.Pos
	msg string
}

type Reports []Report

func (l Reports) Len() int           { return len(l) }
func (l Reports) Less(i, j int) bool { return l[i].pos < l[j].pos }
func (l Reports) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

func reportErr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	exitCode = 2
}
