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

# Use library image once https://github.com/OpenLiberty/ci.docker/issues/241
# is fixed
FROM gcr.io/istio-testing/websphere-liberty:22.0.0.8-full-java8-ibmjava

ENV SERVERDIRNAME reviews

COPY ./servers/LibertyProjectServer /opt/ol/wlp/usr/servers/defaultServer/

RUN /opt/ol/wlp/bin/featureUtility installServerFeatures  --acceptLicense /opt/ol/wlp/usr/servers/defaultServer/server.xml && \
    chmod -R g=rwx /opt/ol/wlp/output/defaultServer/

ARG service_version
ARG enable_ratings
ARG star_color
ENV SERVICE_VERSION ${service_version:-v1}
ENV ENABLE_RATINGS ${enable_ratings:-false}
ENV STAR_COLOR ${star_color:-black}

CMD ["/opt/ol/wlp/bin/server", "run", "defaultServer"]
