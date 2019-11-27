# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

# Copyright Istio Authors
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

FINDFILES=find . \( -path ./common-protos -o -path ./.git -o -path ./.github -o -path ./licenses \) -prune -o -type f
XARGS = xargs -0 -r

lint-dockerfiles:
	@${FINDFILES} -name 'Dockerfile*' -print0 | ${XARGS} hadolint -c ./common/config/.hadolint.yml

lint-scripts:
	@${FINDFILES} -name '*.sh' -print0 | ${XARGS} shellcheck

lint-yaml:
	@${FINDFILES} \( -name '*.yml' -o -name '*.yaml' \) -print0 | ${XARGS} grep -L -e "{{" | xargs -r yamllint -c ./common/config/.yamllint.yml

lint-helm:
	@${FINDFILES} -name 'Chart.yaml' -print0 | ${XARGS} -L 1 dirname | xargs -r helm lint --strict

lint-copyright-banner:
	@${FINDFILES} \( -name '*.go' -o -name '*.cc' -o -name '*.h' -o -name '*.proto' -o -name '*.py' -o -name '*.sh' \) \( ! \( -name '*.gen.go' -o -name '*.pb.go' -o -name '*_pb2.py' \) \) -print0 |\
		${XARGS} common/scripts/lint_copyright_banner.sh

lint-go:
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} common/scripts/lint_go.sh

lint-python:
	@${FINDFILES} -name '*.py' \( ! \( -name '*_pb2.py' \) \) -print0 | ${XARGS} autopep8 --max-line-length 160 --exit-code -d

lint-markdown:
	@${FINDFILES} -name '*.md' -print0 | ${XARGS} mdl --ignore-front-matter --style common/config/mdl.rb
	@${FINDFILES} -name '*.md' -print0 | ${XARGS} awesome_bot --skip-save-results --allow_ssl --allow-timeout --allow-dupe --allow-redirect --white-list ${MARKDOWN_LINT_WHITELIST}

lint-sass:
	@${FINDFILES} -name '*.scss' -print0 | ${XARGS} sass-lint -c common/config/sass-lint.yml --verbose

lint-typescript:
	@${FINDFILES} -name '*.ts' -print0 | ${XARGS} tslint -c common/config/tslint.json

lint-protos:
	@if test -d common-protos; then $(FINDFILES) -name '*.proto' -print0 | $(XARGS) -L 1 prototool lint --protoc-bin-path=/usr/bin/protoc --protoc-wkt-path=common-protos; fi

lint-licenses:
	@-go mod download
	@license-lint --config common/config/license-lint.yml

lint-all: lint-dockerfiles lint-scripts lint-yaml lint-helm lint-copyright-banner lint-go lint-python lint-markdown lint-sass lint-typescript lint-protos lint-licenses

tidy-go:
	@go mod tidy

format-go: tidy-go
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} goimports -w -local "istio.io"

format-python:
	@${FINDFILES} -name '*.py' -print0 | ${XARGS} autopep8 --max-line-length 160 --aggressive --aggressive -i

format-protos:
	@$(FINDFILES) -name '*.proto' -print0 | $(XARGS) -L 1 prototool format -w

dump-licenses:
	@go mod download
	@license-lint --config common/config/license-lint.yml --report

dump-licenses-csv:
	@go mod download
	@license-lint --config common/config/license-lint.yml --csv

mirror-licenses:
	@go mod download
	@rm -fr licenses
	@license-lint --mirror

TMP := $(shell mktemp -d -u)
UPDATE_BRANCH ?= "master"

update-common:
	@mkdir -p $(TMP)
	@git clone -q --depth 1 --single-branch --branch $(UPDATE_BRANCH) https://github.com/istio/common-files $(TMP)/common-files
	@cd $(TMP)/common-files ; git rev-parse HEAD >files/common/.commonfiles.sha
	@rm -fr common
	@cp -a $(TMP)/common-files/files/* $(shell pwd)
	@rm -fr $(TMP)/common-files

update-common-protos:
	@mkdir -p $(TMP)
	@git clone -q --depth 1 --single-branch --branch $(UPDATE_BRANCH) https://github.com/istio/common-files $(TMP)/common-files
	@cd $(TMP)/common-files ; git rev-parse HEAD > common-protos/.commonfiles.sha
	@rm -fr common-protos
	@cp -a $(TMP)/common-files/common-protos $(shell pwd)
	@rm -fr $(TMP)/common-files

check-clean-repo:
	@common/scripts/check_clean_repo.sh

.PHONY: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go lint-python lint-helm lint-markdown lint-sass lint-typescript lint-protos lint-all format-go format-python format-protos update-common update-common-protos lint-licenses dump-licenses dump-licenses-csv check-clean-repo
