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

#include "src/envoy/mixer/utils.h"
#include "mixer/v1/attributes.pb.h"

namespace Envoy {
namespace Http {
namespace Utils {

namespace {

const std::string kSPIFFEPrefix("spiffe://");

}  // namespace

std::map<std::string, std::string> ExtractHeaders(
    const HeaderMap& header_map, const std::set<std::string>& exclusives) {
  std::map<std::string, std::string> headers;
  struct Context {
    Context(const std::set<std::string>& exclusives,
            std::map<std::string, std::string>& headers)
        : exclusives(exclusives), headers(headers) {}
    const std::set<std::string>& exclusives;
    std::map<std::string, std::string>& headers;
  };
  Context ctx(exclusives, headers);
  header_map.iterate(
      [](const HeaderEntry& header, void* context) -> HeaderMap::Iterate {
        Context* ctx = static_cast<Context*>(context);
        if (ctx->exclusives.count(header.key().c_str()) == 0) {
          ctx->headers[header.key().c_str()] = header.value().c_str();
        }
        return HeaderMap::Iterate::Continue;
      },
      &ctx);
  return headers;
}

bool GetIpPort(const Network::Address::Ip* ip, std::string* str_ip, int* port) {
  if (ip) {
    *port = ip->port();
    if (ip->ipv4()) {
      uint32_t ipv4 = ip->ipv4()->address();
      *str_ip = std::string(reinterpret_cast<const char*>(&ipv4), sizeof(ipv4));
      return true;
    }
    if (ip->ipv6()) {
      std::array<uint8_t, 16> ipv6 = ip->ipv6()->address();
      *str_ip = std::string(reinterpret_cast<const char*>(ipv6.data()), 16);
      return true;
    }
  }
  return false;
}

bool GetSourceUser(const Network::Connection* connection, std::string* user) {
  if (connection) {
    Ssl::Connection* ssl = const_cast<Ssl::Connection*>(connection->ssl());
    if (ssl != nullptr) {
      std::string result = ssl->uriSanPeerCertificate();
      if (result.length() >= kSPIFFEPrefix.length() &&
          result.compare(0, kSPIFFEPrefix.length(), kSPIFFEPrefix) == 0) {
        // Strip out the prefix "spiffe://" in the identity.
        *user = result.substr(kSPIFFEPrefix.size());
      } else {
        *user = result;
      }
      return true;
    }
  }
  return false;
}

}  // namespace Utils
}  // namespace Http
}  // namespace Envoy
