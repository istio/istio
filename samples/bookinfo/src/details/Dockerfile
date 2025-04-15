# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

FROM ruby:3.4.3-slim

WORKDIR /opt/microservices
COPY Gemfile Gemfile.lock /opt/microservices/
RUN bundle install

COPY details.rb /opt/microservices/

ARG service_version
ENV SERVICE_VERSION=${service_version:-v1}
ARG enable_external_book_service
ENV ENABLE_EXTERNAL_BOOK_SERVICE=${enable_external_book_service:-false}

EXPOSE 9080

CMD ["ruby", "details.rb", "9080"]

USER 1000
