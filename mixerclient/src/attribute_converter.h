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

#include <unordered_map>

#include "include/attribute.h"
#include "mixer/v1/service.pb.h"

namespace istio {
namespace mixer_client {

// A attribute batch converter for report.
class BatchConverter {
 public:
  virtual ~BatchConverter() {}

  // Add an attribute set to the batch.
  // Return false if it could not be added for delta update.
  virtual bool Add(const Attributes& attributes) = 0;

  // Get the batched size.
  virtual int size() const = 0;

  // Finish the batch and create the batched report request.
  virtual std::unique_ptr<::istio::mixer::v1::ReportRequest> Finish() = 0;
};

// Convert attributes from struct to protobuf
class AttributeConverter {
 public:
  AttributeConverter();

  void Convert(const Attributes& attributes,
               ::istio::mixer::v1::Attributes* attributes_pb) const;

  // Create a batch converter.
  std::unique_ptr<BatchConverter> CreateBatchConverter() const;

  int global_word_count() const { return global_dict_.size(); }

  // Shrink global dictionary to the first version.
  void ShrinkGlobalDictionary();

 private:
  std::unordered_map<std::string, int> global_dict_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_ATTRIBUTE_CONVERTER_H
