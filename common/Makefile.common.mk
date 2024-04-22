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

FINDFILES=find . \( -path ./common-protos -o -path ./.git -o -path ./out -o -path ./.github -o -path ./licenses -o -path ./vendor $(if $(strip ${FINDFILES_IGNORE}), -o ${FINDFILES_IGNORE}) \) -prune -o -type f

XARGS = xargs -0 -r

lint-dockerfiles:
	@${FINDFILES} -name 'Dockerfile*' -print0 | ${XARGS} hadolint -c ./common/config/.hadolint.yml

lint-scripts:
	@${FINDFILES} -name '*.sh' -print0 | ${XARGS} -n 256 shellcheck

lint-yaml:
	@${FINDFILES} \( -name '*.yml' -o -name '*.yaml' \) -not -exec grep -q -e "{{" {} \; -print0 | ${XARGS} yamllint -c ./common/config/.yamllint.yml

lint-helm:
	@${FINDFILES} -name 'Chart.yaml' -print0 | ${XARGS} -L 1 dirname | xargs -r helm lint --strict

lint-copyright-banner:
	@${FINDFILES} \( -name '*.go' -o -name '*.cc' -o -name '*.h' -o -name '*.proto' -o -name '*.py' -o -name '*.sh' -o -name '*.rs' \) \( ! \( -name '*.gen.go' -o -name '*.pb.go' -o -name '*_pb2.py' \) \) -print0 |\
		${XARGS} common/scripts/lint_copyright_banner.sh

fix-copyright-banner:
	@${FINDFILES} \( -name '*.go' -o -name '*.cc' -o -name '*.h' -o -name '*.proto' -o -name '*.py' -o -name '*.sh' -o -name '*.rs' \) \( ! \( -name '*.gen.go' -o -name '*.pb.go' -o -name '*_pb2.py' \) \) -print0 |\
		${XARGS} common/scripts/fix_copyright_banner.sh

lint-go:
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} common/scripts/lint_go.sh

lint-python:
	@${FINDFILES} -name '*.py' \( ! \( -name '*_pb2.py' \) \) -print0 | ${XARGS} autopep8 --max-line-length 160 --exit-code -d

lint-markdown:
	@${FINDFILES} -name '*.md' -print0 | ${XARGS} mdl --ignore-front-matter --style common/config/mdl.rb

lint-links:
	@${FINDFILES} -name '*.md' -print0 | ${XARGS} awesome_bot --skip-save-results --allow_ssl --allow-timeout --allow-dupe --allow-redirect --white-list ${MARKDOWN_LINT_ALLOWLIST}

lint-sass:
	@${FINDFILES} -name '*.scss' -print0 | ${XARGS} sass-lint -c common/config/sass-lint.yml --verbose

lint-typescript:
	@${FINDFILES} -name '*.ts' -print0 | ${XARGS} tslint -c common/config/tslint.json

lint-licenses:
	@if test -d licenses; then license-lint --config common/config/license-lint.yml; fi

lint-all: lint-dockerfiles lint-scripts lint-yaml lint-helm lint-copyright-banner lint-go lint-python lint-markdown lint-sass lint-typescript lint-licenses

tidy-go:
	@find -name go.mod -execdir go mod tidy \;

mod-download-go:
	@-GOFLAGS="-mod=readonly" find -name go.mod -execdir go mod download \;
# go mod tidy is needed with Golang 1.16+ as go mod download affects go.sum
# https://github.com/golang/go/issues/43994
	@find -name go.mod -execdir go mod tidy \;

format-go: tidy-go
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} common/scripts/format_go.sh

format-python:
	@${FINDFILES} -name '*.py' -print0 | ${XARGS} autopep8 --max-line-length 160 --aggressive --aggressive -i

dump-licenses: mod-download-go
	@license-lint --config common/config/license-lint.yml --report

dump-licenses-csv: mod-download-go
	@license-lint --config common/config/license-lint.yml --csv

mirror-licenses: mod-download-go
	@rm -fr licenses
	@license-lint --mirror

TMP := $(shell mktemp -d -u)
UPDATE_BRANCH ?= "release-1.22"

BUILD_TOOLS_ORG ?= "istio"

update-common:
	@mkdir -p $(TMP)
	@git clone -q --depth 1 --single-branch --branch $(UPDATE_BRANCH) https://github.com/$(BUILD_TOOLS_ORG)/common-files $(TMP)/common-files
	@cd $(TMP)/common-files ; git rev-parse HEAD >files/common/.commonfiles.sha
	@rm -fr common
# istio/community has its own CONTRIBUTING.md file.
	@CONTRIB_OVERRIDE=$(shell grep -l "istio/community/blob/master/CONTRIBUTING.md" CONTRIBUTING.md)
	@if [ "$(CONTRIB_OVERRIDE)" != "CONTRIBUTING.md" ]; then\
		rm $(TMP)/common-files/files/CONTRIBUTING.md;\
	fi
# istio/istio.io uses the  Creative Commons Attribution 4.0 license. Don't update LICENSE with the common Apache license.
	@LICENSE_OVERRIDE=$(shell grep -l "Creative Commons Attribution 4.0 International Public License" LICENSE)
	@if [ "$(LICENSE_OVERRIDE)" != "LICENSE" ]; then\
		rm $(TMP)/common-files/files/LICENSE;\
	fi
	@cp -a $(TMP)/common-files/files/* $(TMP)/common-files/files/.devcontainer $(TMP)/common-files/files/.gitattributes $(shell pwd)
	@rm -fr $(TMP)/common-files
	@$(or $(COMMONFILES_POSTPROCESS), true)

check-clean-repo:
	@common/scripts/check_clean_repo.sh

tidy-docker:
	@docker image prune --all --force --filter="label=io.istio.repo=https://github.com/istio/tools" --filter="label!=io.istio.version=$(IMAGE_VERSION)"

# help works by looking over all Makefile includes matching `target: ## comment` regex and outputting them
help: ## Show this help
	@egrep -h '^[a-zA-Z_\.-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort  | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go lint-python lint-helm lint-markdown lint-sass lint-typescript lint-all format-go format-python update-common lint-licenses dump-licenses dump-licenses-csv check-clean-repo tidy-docker help tidy-go mod-download-go
