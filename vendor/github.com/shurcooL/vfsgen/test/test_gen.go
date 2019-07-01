// +build generate

package main

import (
	"log"
	"net/http"

	"github.com/shurcooL/vfsgen"
	"golang.org/x/tools/godoc/vfs/httpfs"
	"golang.org/x/tools/godoc/vfs/mapfs"
)

func main() {
	var fs http.FileSystem = httpfs.New(mapfs.New(map[string]string{
		"sample-file.txt":                "This file compresses well. Blaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaah!",
		"not-worth-compressing-file.txt": "Its normal contents are here.",
		"folderA/file1.txt":              "Stuff in /folderA/file1.txt.",
		"folderA/file2.txt":              "Stuff in /folderA/file2.txt.",
		"folderB/folderC/file3.txt":      "Stuff in /folderB/folderC/file3.txt.",
		// TODO: Empty folder somehow?
		//"folder-empty/":                  "",
	}))

	err := vfsgen.Generate(fs, vfsgen.Options{
		Filename:    "test_vfsdata_test.go",
		PackageName: "test_test",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
