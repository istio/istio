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

#ifndef ISTIO_MIXERCLIENT_REFERENCED_H_
#define ISTIO_MIXERCLIENT_REFERENCED_H_

#include <vector>

#include "proxy/include/istio/utils/md5.h"
#include "mixer/v1/check.pb.h"

namespace istio {
namespace mixerclient {

// The object to store referenced attributes used by Mixer server.
// Mixer client cache should only use referenced attributes
// in its cache (for both Check cache and quota cache).
class Referenced {
 public:
  // Fill the object from the protobuf from Check response.
  // Return false if any attribute names could not be decoded from client
  // global dictionary.
  bool Fill(const ::istio::mixer::v1::Attributes &attributes,
            const ::istio::mixer::v1::ReferencedAttributes &reference);

  // Calculate a cache signature for the attributes.
  // Return false if attributes are mismatched, such as "absence" attributes
  // present
  // or "exact" match attributes don't present.
  bool Signature(const ::istio::mixer::v1::Attributes &attributes,
                 const std::string &extra_key, std::string *signature) const;

  // A hash value to identify an instance.
  std::string Hash() const;

  // For debug logging only.
  std::string DebugString() const;

 private:
  // Return true if all absent keys are not in the attributes.
  bool CheckAbsentKeys(const ::istio::mixer::v1::Attributes &attributes) const;

  // Return true if all exact keys are in the attributes.
  bool CheckExactKeys(const ::istio::mixer::v1::Attributes &attributes) const;

  // Do the actual signature calculation.
  void CalculateSignature(const ::istio::mixer::v1::Attributes &attributes,
                          const std::string &extra_key,
                          std::string *signature) const;

  // Holds reference to an attribute and potentially a map key
  struct AttributeRef {
    // name of the attribute
    std::string name;
    // only used if attribute is a stringMap
    std::string map_key;

    // make vector<AttributeRef> sortable
    bool operator<(const AttributeRef &b) const {
      int cmp = name.compare(b.name);
      if (cmp == 0) {
        return map_key.compare(b.map_key) < 0;
      }

      return cmp < 0;
    };
  };

  // The keys should be absence.
  std::vector<AttributeRef> absence_keys_;

  // The keys should match exactly.
  std::vector<AttributeRef> exact_keys_;

  // Updates hasher with keys
  static void UpdateHash(const std::vector<AttributeRef> &keys,
                         utils::MD5 *hasher);
};

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_REFERENCED_H_
