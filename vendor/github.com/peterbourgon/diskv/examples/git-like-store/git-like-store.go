package main

/* This example uses a more advanced transform function that simulates a bit
 how Git stores objects:

* places hash-like keys under the objects directory
* any other key is placed in the base directory. If the key
* contains slashes, these are converted to subdirectories

*/

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/peterbourgon/diskv/v3"
)

var hex40 = regexp.MustCompile("[0-9a-fA-F]{40}")

func hexTransform(s string) *diskv.PathKey {
	if hex40.MatchString(s) {
		return &diskv.PathKey{Path: []string{"objects", s[0:2]},
			FileName: s,
		}
	}

	folders := strings.Split(s, "/")
	lfolders := len(folders)
	if lfolders > 1 {
		return &diskv.PathKey{Path: folders[:lfolders-1],
			FileName: folders[lfolders-1],
		}
	}

	return &diskv.PathKey{Path: []string{},
		FileName: s,
	}
}

func hexInverseTransform(pathKey *diskv.PathKey) string {
	if hex40.MatchString(pathKey.FileName) {
		return pathKey.FileName
	}

	if len(pathKey.Path) == 0 {
		return pathKey.FileName
	}

	return strings.Join(pathKey.Path, "/") + "/" + pathKey.FileName
}

func main() {
	d := diskv.New(diskv.Options{
		BasePath:          "my-data-dir",
		AdvancedTransform: hexTransform,
		InverseTransform:  hexInverseTransform,
		CacheSizeMax:      1024 * 1024,
	})

	// Write some text to the key "alpha/beta/gamma".
	key := "1bd88421b055327fcc8660c76c4894c4ea4c95d7"
	d.WriteString(key, "¡Hola!") // will be stored in "<basedir>/objects/1b/1bd88421b055327fcc8660c76c4894c4ea4c95d7"

	d.WriteString("refs/heads/master", "some text") // will be stored in "<basedir>/refs/heads/master"

	fmt.Println("Enumerating All keys:")
	c := d.Keys(nil)

	for key := range c {
		value := d.ReadString(key)
		fmt.Printf("Key: %s, Value: %s\n", key, value)
	}
}
