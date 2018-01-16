
all: build run

build:
	go build -o protoc-gen-docs *.go

run:
	rm -fr jekyll page fragment
	mkdir jekyll page fragment
	protoc --plugin=./protoc-gen-docs --docs_out=warnings=false,emit_yaml=true,mode=html_page:page/. testdata/test1.proto testdata/test2.proto testdata/test3.proto

more:
	protoc --plugin=./protoc-gen-docs --docs_out=warnings=false,mode=jekyll_html:jekyll/. testdata/test1.proto testdata/test2.proto 	testdata/test3.proto
	protoc --plugin=./protoc-gen-docs --docs_out=warnings=false,mode=html_fragment:fragment/. testdata/test1.proto testdata/test2.proto testdata/test3.proto

clean:
	@rm -fr jekyll page fragment protoc-gen-docs
