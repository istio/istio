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

#ifndef MIXER_CLIENT_REFERENCED_H_
#define MIXER_CLIENT_REFERENCED_H_

#include <vector>

#include "mixer/v1/check.pb.h"

namespace istio {
namespace mixer_client {

// The object to store referenced attributes used by Mixer server.
// Mixer client cache should only use referenced attributes
// in its cache (for both Check cache and quota cache).
class Referenced {
 public:
  // Fill the object from the protobuf from Check response.
  // Return false if any attribute names could not be decoded from client
  // global dictionary.
  bool Fill(const ::istio::mixer::v1::ReferencedAttributes& reference);

  // Calculate a cache signature for the attributes.
  // Return false if attributes are mismatched, such as "absence" attributes
  // present
  // or "exact" match attributes don't present.
  bool Signature(const ::istio::mixer::v1::Attributes& attributes,
                 const std::string& extra_key, std::string* signature) const;

  // A hash value to identify an instance.
  std::string Hash() const;

  // For debug logging only.
  std::string DebugString() const;

 private:
  // The keys should be absence.
  std::vector<std::string> absence_keys_;
  // The keys should match exactly.
  std::vector<std::string> exact_keys_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXER_CLIENT_REFERENCED_H_
