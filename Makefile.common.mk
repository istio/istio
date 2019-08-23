# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

# Copyright 2019 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

lint-dockerfiles:
	@find . -path ./vendor -prune -o -type f -name 'Dockerfile*' -print0 | xargs -0 hadolint -c ./.hadolint.yml

lint-scripts:
	@find . -path ./vendor -prune -o -type f -name '*.sh' -print0 | xargs -0 shellcheck

lint-yaml:
	@find . -path ./vendor -prune -o -type f -name '*.yml' -print0 | xargs -0 yamllint -c ./.yamllint.yml
	@find . -path ./vendor -prune -o -type f -name '*.yaml' -print0 | xargs -0 yamllint -c ./.yamllint.yml

lint-copyright-banner:
	@scripts/lint_copyright_banner.sh

lint-go:
	@golangci-lint run -j 8 -v ./...

format-go:
	@goimports -w -local "istio.io" $(shell find . -type f -name '*.go' ! -name '*.gen.go' ! -name '*.pb.go' )

update-common:
	@git clone --depth 1 --single-branch --branch master https://github.com/istio/common-files
	@cd common-files ; git rev-parse HEAD >.commonfiles.sha
	@cp -r common-files/files/* common-files/.commonfiles.sha common-files/files/.[^.]* .
	@rm -fr common-files
	@touch Makefile.overrides.mk  # make sure this at least exists
	# temporary, until cleaned up in all repos
	@rm -fr scripts/check_license.sh

.PHONY: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go format-go update-common
