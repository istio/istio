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

#ifndef MIXERCLIENT_ATTRIBUTE_COMPRESSOR_H
#define MIXERCLIENT_ATTRIBUTE_COMPRESSOR_H

#include <unordered_map>

#include "mixer/v1/attributes.pb.h"
#include "mixer/v1/report.pb.h"

namespace istio {
namespace mixer_client {

// A class to store global dictionary
class GlobalDictionary {
 public:
  GlobalDictionary();

  // Lookup the index, return true if found.
  bool GetIndex(const std::string word, int* index) const;

  // Shrink the global dictioanry
  void ShrinkToBase();

  int size() const { return top_index_; }

 private:
  std::unordered_map<std::string, int> global_dict_;
  // the last index of the global dictionary.
  // If mis-matched with server, it will set to base
  int top_index_;
};

// A attribute batch compressor for report.
class BatchCompressor {
 public:
  virtual ~BatchCompressor() {}

  // Add an attribute set to the batch.
  // Return false if it could not be added for delta update.
  virtual bool Add(const ::istio::mixer::v1::Attributes& attributes) = 0;

  // Get the batched size.
  virtual int size() const = 0;

  // Finish the batch and create the batched report request.
  virtual std::unique_ptr<::istio::mixer::v1::ReportRequest> Finish() = 0;
};

// Compress attributes.
class AttributeCompressor {
 public:
  void Compress(const ::istio::mixer::v1::Attributes& attributes,
                ::istio::mixer::v1::CompressedAttributes* attributes_pb) const;

  // Create a batch compressor.
  std::unique_ptr<BatchCompressor> CreateBatchCompressor() const;

  int global_word_count() const { return global_dict_.size(); }

  // Shrink global dictionary to the first version.
  void ShrinkGlobalDictionary() { global_dict_.ShrinkToBase(); }

 private:
  GlobalDictionary global_dict_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_ATTRIBUTE_COMPRESSOR_H
