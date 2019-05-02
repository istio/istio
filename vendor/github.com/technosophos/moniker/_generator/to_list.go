package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	for _, f := range os.Args[1:] {
		fmt.Printf("Loading %s\n", f)
		data, err := os.Open(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot read file %s: %s", f, err)
			os.Exit(1)
		}
		if err := toList(f, data); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to convert %s: %s", f, err)
		}
		data.Close()
	}
}

func toList(name string, data *os.File) error {
	b := filepath.Base(name)
	parts := strings.SplitN(b, ".", 2)
	aname := strings.Title(parts[0])

	outname := fmt.Sprintf("./%s_list.go", parts[0])
	out, err := os.Create(outname)
	if err != nil {
		return err
	}

	pkg := os.Getenv("GOPACKAGE")
	if pkg == "" {
		pkg = "main"
	}

	fmt.Fprintf(out, "package %s\n\n// %s is a generated list of words.\nvar %s = []string{\n", pkg, aname, aname)
	scanner := bufio.NewScanner(data)
	for scanner.Scan() {
		fmt.Fprintf(out, "\t%q,\n", scanner.Text())
	}
	fmt.Fprintln(out, "}")
	out.Close()

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
