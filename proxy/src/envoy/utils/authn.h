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
#include "common/protobuf/protobuf.h"
#include "envoy/api/v2/core/base.pb.h"
#include "google/protobuf/struct.pb.h"
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Utils {

class Authentication : public Logger::Loggable<Logger::Id::filter> {
 public:
  // Save authentication attributes into the data Struct.
  static void SaveAuthAttributesToStruct(const istio::authn::Result& result,
                                         ::google::protobuf::Struct& data);

  // Returns a pointer to the authentication result from metadata. Typically,
  // the input metadata is the request info's dynamic metadata. Authentication
  // result, if available, is stored under authentication filter metdata.
  // Returns nullptr if there is no data for that filter.
  static const ProtobufWkt::Struct* GetResultFromMetadata(
      const envoy::api::v2::core::Metadata& metadata);
};

}  // namespace Utils
}  // namespace Envoy
