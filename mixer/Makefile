## Copyright 2016 Google Inc.
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.

build:
	@bazel build ...:all

clean:
	@bazel clean

test:
	@bazel test ...

# The lint rule:
#   - Builds the mixer so we get generated .pb.go files for the mixer API
#   - Installs gometalinter and runs it to install all its linters
#   - Hackily copies all the generated .pb.go files into the istio.io/api
#     directory
#   - Runs gometalinter
#   - Removes the copied .pb.go files
lint: build
	@go get -u github.com/alecthomas/gometalinter
	@gometalinter --install >/dev/null
	@cp -f bazel-genfiles/external/com_github_istio_api/mixer/api/v1/*pb.go ../api/mixer/api/v1/
	@gometalinter --disable-all\
		--enable=aligncheck\
		--enable=deadcode\
		--enable=errcheck\
		--enable=gas\
		--enable=goconst\
		--enable=gofmt\
		--enable=gosimple\
		--enable=ineffassign\
		--enable=interfacer\
		--enable=lll --line-length=160\
		--enable=misspell\
		--enable=staticcheck\
		--enable=structcheck\
		--enable=unconvert\
		--enable=unused\
		--enable=varcheck\
		--enable=vetshadow\
		./...
	@rm -f ../api/mixer/api/v1/*.pb.go

# These generate warnings which we should fix, and then should enable the linters
# --enable=dupl
# --enable=gocyclo
# --enable=golint

# These don't seem interesting
# --enable=goimports
# --enable=gotype

# Add this linter once its current build failure is fixed
# github.com/3rf/codecoroner
