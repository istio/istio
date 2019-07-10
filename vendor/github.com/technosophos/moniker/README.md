# Monicker: Generate Cute Random Names
[![Stability: Maintenance](https://masterminds.github.io/stability/maintenance.svg)](https://masterminds.github.io/stability/maintenance.html)

Monicker is a tiny Go library for automatically naming things.

```console
$ for i in [ 1 2 3 4 5 ]; do go run _example/namer.go; done
Your name is "eating iguana"
Your name is "old whale"
Your name is "wrinkled lion"
Your name is "wintering skunk"
Your name is "kneeling giraffe"
Your name is "icy hummingbird"
Your name is "wizened guppy"
```

## Library Usage

Easily use Monicker in your code. Here's the complete code behind the
tool above:

```go
package main

import (
	"fmt"

	"github.com/technosophos/moniker"
)

func main() {
	n := moniker.New()
	fmt.Printf("Your name is %q\n", n.Name())
}
```

Since Monicker compiles the name list into the application, there's no
requirement that your app has supporting files.

## Customizing the Words

Monicker ships with a couple of word lists that were written and
approved by a group of giggling school children (and their dad). We
built a lighthearted list based on animals and descriptive words (mostly
adjectives).

First, we welcome contributions to our list. Note that we currate the
list to avoid things that might be offensive. (And if you get an
offensive pairing, please open an issue!)

Second, we wanted to make it easy for you to modify or build your own
wordlist. The script in `_generator` can take word lists and convert
them to Go code that Monicker understands.
