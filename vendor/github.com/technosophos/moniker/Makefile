GOPACKAGE="moniker"

.PHONY: sort
sort:
	sort -u -o animals.txt animals.txt
	sort -u -o descriptors.txt descriptors.txt

.PHONY: build
build: sort
	GOPACKAGE=$(GOPACKAGE) go run _generator/to_list.go ./animals.txt
	GOPACKAGE=$(GOPACKAGE) go run _generator/to_list.go ./descriptors.txt
