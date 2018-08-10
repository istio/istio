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

#include "src/envoy/utils/utils.h"
#include "mixer/v1/attributes.pb.h"

using ::google::protobuf::Message;
using ::google::protobuf::Struct;
using ::google::protobuf::util::Status;

namespace Envoy {
namespace Utils {

namespace {

const std::string kSPIFFEPrefix("spiffe://");

// Per-host opaque data field
const std::string kPerHostMetadataKey("istio");

// Attribute field for per-host data override
const std::string kMetadataDestinationUID("uid");

}  // namespace

void ExtractHeaders(const Http::HeaderMap& header_map,
                    const std::set<std::string>& exclusives,
                    std::map<std::string, std::string>& headers) {
  struct Context {
    Context(const std::set<std::string>& exclusives,
            std::map<std::string, std::string>& headers)
        : exclusives(exclusives), headers(headers) {}
    const std::set<std::string>& exclusives;
    std::map<std::string, std::string>& headers;
  };
  Context ctx(exclusives, headers);
  header_map.iterate(
      [](const Http::HeaderEntry& header,
         void* context) -> Http::HeaderMap::Iterate {
        Context* ctx = static_cast<Context*>(context);
        if (ctx->exclusives.count(header.key().c_str()) == 0) {
          ctx->headers[header.key().c_str()] = header.value().c_str();
        }
        return Http::HeaderMap::Iterate::Continue;
      },
      &ctx);
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
      absl::uint128 ipv6 = ip->ipv6()->address();
      *str_ip = std::string(reinterpret_cast<const char*>(&ipv6), 16);
      return true;
    }
  }
  return false;
}

bool GetDestinationUID(const envoy::api::v2::core::Metadata& metadata,
                       std::string* uid) {
  const auto filter_it = metadata.filter_metadata().find(kPerHostMetadataKey);
  if (filter_it == metadata.filter_metadata().end()) {
    return false;
  }
  const Struct& struct_pb = filter_it->second;
  const auto fields_it = struct_pb.fields().find(kMetadataDestinationUID);
  if (fields_it == struct_pb.fields().end()) {
    return false;
  }
  *uid = fields_it->second.string_value();
  return true;
}

bool GetPrincipal(const Network::Connection* connection, bool peer,
                  std::string* principal) {
  if (connection) {
    Ssl::Connection* ssl = const_cast<Ssl::Connection*>(connection->ssl());
    if (ssl != nullptr) {
      std::string result;
      if (peer) {
        result = ssl->uriSanPeerCertificate();
      } else {
        result = ssl->uriSanLocalCertificate();
      }

      if (result.empty()) {  // empty result is not allowed
        return false;
      }
      if (result.length() >= kSPIFFEPrefix.length() &&
          result.compare(0, kSPIFFEPrefix.length(), kSPIFFEPrefix) == 0) {
        // Strip out the prefix "spiffe://" in the identity.
        *principal = result.substr(kSPIFFEPrefix.size());
      } else {
        *principal = result;
      }
      return true;
    }
  }
  return false;
}

bool IsMutualTLS(const Network::Connection* connection) {
  return connection != nullptr && connection->ssl() != nullptr &&
         connection->ssl()->peerCertificatePresented();
}

bool GetRequestedServerName(const Network::Connection* connection,
                            std::string* name) {
  if (connection && !connection->requestedServerName().empty()) {
    *name = std::string(connection->requestedServerName());
    return true;
  }

  return false;
}

Status ParseJsonMessage(const std::string& json, Message* output) {
  ::google::protobuf::util::JsonParseOptions options;
  options.ignore_unknown_fields = true;
  return ::google::protobuf::util::JsonStringToMessage(json, output, options);
}

}  // namespace Utils
}  // namespace Envoy
