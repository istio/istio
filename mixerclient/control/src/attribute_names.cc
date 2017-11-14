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

#include "attribute_names.h"

namespace istio {
namespace mixer_control {

// Define attribute names
const char AttributeName::kSourceUser[] = "source.user";

const char AttributeName::kRequestHeaders[] = "request.headers";
const char AttributeName::kRequestHost[] = "request.host";
const char AttributeName::kRequestMethod[] = "request.method";
const char AttributeName::kRequestPath[] = "request.path";
const char AttributeName::kRequestReferer[] = "request.referer";
const char AttributeName::kRequestScheme[] = "request.scheme";
const char AttributeName::kRequestSize[] = "request.size";
const char AttributeName::kRequestTime[] = "request.time";
const char AttributeName::kRequestUserAgent[] = "request.useragent";

const char AttributeName::kResponseCode[] = "response.code";
const char AttributeName::kResponseDuration[] = "response.duration";
const char AttributeName::kResponseHeaders[] = "response.headers";
const char AttributeName::kResponseSize[] = "response.size";
const char AttributeName::kResponseTime[] = "response.time";

// TCP attributes
// Downstream tcp connection: source ip/port.
const char AttributeName::kSourceIp[] = "source.ip";
const char AttributeName::kSourcePort[] = "source.port";
// Upstream tcp connection: destionation ip/port.
const char AttributeName::kDestinationIp[] = "destination.ip";
const char AttributeName::kDestinationPort[] = "destination.port";
const char AttributeName::kConnectionReceviedBytes[] =
    "connection.received.bytes";
const char AttributeName::kConnectionReceviedTotalBytes[] =
    "connection.received.bytes_total";
const char AttributeName::kConnectionSendBytes[] = "connection.sent.bytes";
const char AttributeName::kConnectionSendTotalBytes[] =
    "connection.sent.bytes_total";
const char AttributeName::kConnectionDuration[] = "connection.duration";

// Context attributes
const char AttributeName::kContextProtocol[] = "context.protocol";
const char AttributeName::kContextTime[] = "context.time";

// Check status code.
const char AttributeName::kCheckStatusCode[] = "check.status";

}  // namespace mixer_control
}  // namespace istio
