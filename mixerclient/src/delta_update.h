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

#ifndef MIXERCLIENT_DELTA_UPDATE_H
#define MIXERCLIENT_DELTA_UPDATE_H

#include "mixer/v1/attributes.pb.h"

#include <memory>

namespace istio {
namespace mixer_client {

// A class to support attribute delta update.
// It has previous attribute values and check
// for the current one.
class DeltaUpdate {
 public:
  virtual ~DeltaUpdate() {}

  // Start a new delta update
  virtual void Start() = 0;

  // Check an attribute, return true if it is in the previous
  // set with same value, so no need to send it again.
  // Each attribute in the current set needs to call this method.
  virtual bool Check(
      int index,
      const ::istio::mixer::v1::Attributes_AttributeValue& value) = 0;

  // Finish a delta update.
  // Return false if delta update is not supported.
  // For example, "deleted" is not supported, if some attributes are
  // missing, delta update will not be supported.
  virtual bool Finish() = 0;

  // Create an instance.
  static std::unique_ptr<DeltaUpdate> Create();

  // Create an no-op instance; an optimization for no delta update cases.
  static std::unique_ptr<DeltaUpdate> CreateNoOp();
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_DELTA_UPDATE_H
