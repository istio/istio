# Copyright 2017 Istio Authors
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

# Ref: https://hub.docker.com/_/python/

FROM python:2

RUN apt-get install libmysqlclient-dev

RUN mkdir -p /opt/microservices
WORKDIR /opt/microservices
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .
EXPOSE 9080
ARG service_version
ENV SERVICE_VERSION ${service_version:-v1}
CMD python productpage.py 9080 $MYSQLADDR
