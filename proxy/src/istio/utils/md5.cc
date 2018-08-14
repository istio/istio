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

#include "include/istio/utils/md5.h"
#include <assert.h>

namespace istio {
namespace utils {

MD5::MD5() : finalized_(false) { MD5_Init(&ctx_); }

MD5& MD5::Update(const void* data, size_t size) {
  // Not update after finalized.
  assert(!finalized_);
  MD5_Update(&ctx_, data, size);
  return *this;
}

std::string MD5::Digest() {
  if (!finalized_) {
    MD5_Final(digest_, &ctx_);
    finalized_ = true;
  }
  return std::string(reinterpret_cast<char*>(digest_), kDigestLength);
}

std::string MD5::DebugString(const std::string& digest) {
  assert(digest.size() == kDigestLength);
  char buf[kDigestLength * 2 + 1];
  char* p = buf;
  for (int i = 0; i < kDigestLength; i++, p += 2) {
    sprintf(p, "%02x", (unsigned char)digest[i]);
  }
  *p = 0;
  return std::string(buf, kDigestLength * 2);
}

std::string MD5::operator()(const void* data, size_t size) {
  return Update(data, size).Digest();
}

}  // namespace utils
}  // namespace istio
