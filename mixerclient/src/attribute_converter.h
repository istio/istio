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

#ifndef MIXERCLIENT_ATTRIBUTE_CONVERTER_H
#define MIXERCLIENT_ATTRIBUTE_CONVERTER_H

#include "include/client.h"

namespace istio {
namespace mixer_client {

// Underlying stream ID.
typedef int64_t StreamID;

// An interface to convert from struct to protobuf.
// It is called by StreamTransport after picking a stream to use.
// It will be implemented by Transport with correct attribute
// context.
template <class RequestType>
class AttributeConverter {
 public:
  // virtual destructor
  virtual ~AttributeConverter() {}

  // Convert attributes from struct to protobuf
  // It requires an attribute context. A different stream_id
  // requires a new attribute context.
  virtual void FillProto(StreamID stream_id, const Attributes& attributes,
                         RequestType* request) = 0;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_ATTRIBUTE_CONVERTER_H
