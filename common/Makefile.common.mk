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

FINDFILES=find . \( -path ./vendor -o -path ./.git \) -prune -o -type f
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
	@golangci-lint run -j 8 -c ./common/config/.golangci.yml

lint-python:
	@${FINDFILES} -name '*.py' -print0 | ${XARGS} autopep8 --max-line-length 160 --exit-code -d

format-go:
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} goimports -w -local "istio.io"

format-python:
	@${FINDFILES} -name '*.py' -print0 | ${XARGS} autopep8 --max-line-length 160 --aggressive --aggressive -i

update-common:
	@git clone --depth 1 --single-branch --branch master https://github.com/istio/common-files
	@cd common-files ; git rev-parse HEAD >files/common/.commonfiles.sha
	@rm -fr common
	@cp -r common-files/files/* .
	@rm -fr common-files

.PHONY: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go lint-pyhton lint-helm format-go format-python update-common
