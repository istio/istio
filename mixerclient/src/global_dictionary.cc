/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "src/global_dictionary.h"

namespace istio {
namespace mixer_client {
namespace {

/*
 * Automatically generated global dictionary from
 * https://github.com/istio/api/blob/master/mixer/v1/global_dictionary.yaml
 * by run:
 *  ./create_global_dictionary.py \
 *    bazel-mixerclient/external/mixerapi_git/mixer/v1/global_dictionary.yaml \
 *      > src/global_dictionary.cc
 */

const std::vector<std::string> kGlobalWords{
    "source.ip",
    "source.port",
    "source.name",
    "source.uid",
    "source.namespace",
    "source.labels",
    "source.user",
    "target.ip",
    "target.port",
    "target.service",
    "target.name",
    "target.uid",
    "target.namespace",
    "target.labels",
    "target.user",
    "request.headers",
    "request.id",
    "request.path",
    "request.host",
    "request.method",
    "request.reason",
    "request.referer",
    "request.scheme",
    "request.size",
    "request.time",
    "request.useragent",
    "response.headers",
    "response.size",
    "response.time",
    "response.duration",
    "response.code",
    ":authority",
    ":method",
    ":path",
    ":scheme",
    ":status",
    "access-control-allow-origin",
    "access-control-allow-methods",
    "access-control-allow-headers",
    "access-control-max-age",
    "access-control-request-method",
    "access-control-request-headers",
    "accept-charset",
    "accept-encoding",
    "accept-language",
    "accept-ranges",
    "accept",
    "access-control-allow",
    "age",
    "allow",
    "authorization",
    "cache-control",
    "content-disposition",
    "content-encoding",
    "content-language",
    "content-length",
    "content-location",
    "content-range",
    "content-type",
    "cookie",
    "date",
    "etag",
    "expect",
    "expires",
    "from",
    "host",
    "if-match",
    "if-modified-since",
    "if-none-match",
    "if-range",
    "if-unmodified-since",
    "keep-alive",
    "last-modified",
    "link",
    "location",
    "max-forwards",
    "proxy-authenticate",
    "proxy-authorization",
    "range",
    "referer",
    "refresh",
    "retry-after",
    "server",
    "set-cookie",
    "strict-transport-sec",
    "transfer-encoding",
    "user-agent",
    "vary",
    "via",
    "www-authenticate",
    "GET",
    "POST",
    "http",
    "envoy",
    "'200'",
    "Keep-Alive",
    "chunked",
    "x-envoy-service-time",
    "x-forwarded-for",
    "x-forwarded-host",
    "x-forwarded-proto",
    "x-http-method-override",
    "x-request-id",
    "x-requested-with",
    "application/json",
    "application/xml",
    "gzip",
    "text/html",
    "text/html; charset=utf-8",
    "text/plain",
    "text/plain; charset=utf-8",
};

}  // namespace

const std::vector<std::string>& GetGlobalWords() { return kGlobalWords; }

}  // namespace mixer_client
}  // namespace istio
