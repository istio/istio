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

FROM python:3.9-slim

WORKDIR /opt/microservices
COPY app.py ./
COPY requirements.txt ./

# install the dependencies and packages in the requirements file
RUN pip install --no-cache-dir -r requirements.txt

# old image had curl and could be used as a sample client if desired
RUN apt-get update \
  && apt-get install curl --no-install-recommends -y \
  && rm -rf /var/lib/apt/lists/*

EXPOSE 5000

ARG service_version
ENV SERVICE_VERSION ${service_version:-v1}

# image will bind on TCP6 by default. In k8s pod spec (in a deployment pod template most likely) override for explicit IPv4 if needed with the command shown below:
# ["gunicorn", "-b", "[0.0.0.0]:5000", "app:app", "-k", "gevent"]
CMD ["gunicorn", "-b", "[::]:5000", "app:app", "-k", "gevent"]
