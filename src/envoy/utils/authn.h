/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "common/common/logger.h"
#include "envoy/http/header_map.h"
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Utils {

class Authentication : public Logger::Loggable<Logger::Id::filter> {
 public:
  // Saves (authentication) result to header with proper encoding (base64). The
  // location is internal implementation detail, and is chosen to avoid possible
  // collision. If header already contains data in that location, function will
  // returns false and data is *not* overwritten.
  static bool SaveResultToHeader(const istio::authn::Result& result,
                                 Http::HeaderMap* headers);

  // Looks up authentication result data in the header. If data is available,
  // decodes and output result proto. Returns false if data is not available, or
  // in bad format.
  static bool FetchResultFromHeader(const Http::HeaderMap& headers,
                                    istio::authn::Result* result);

  // Clears authentication result in header, if exist.
  static void ClearResultInHeader(Http::HeaderMap* headers);

  // Returns true if there is header entry at thelocation that is used to store
  // authentication result. (function does not check for validity of the data
  // though).
  static bool HasResultInHeader(const Http::HeaderMap& headers);

 private:
  // Return the header location key. For testing purpose only.
  static const Http::LowerCaseString& GetHeaderLocation();

  friend class AuthenticationTest;
};

}  // namespace Utils
}  // namespace Envoy
