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

#ifndef ISTIO_UTILS_MD5_H_
#define ISTIO_UTILS_MD5_H_

#include <string.h>
#include <string>
#include "openssl/md5.h"

namespace istio {
namespace utils {

// Define a MD5 Digest by calling OpenSSL
class MD5 {
 public:
  MD5();

  // Updates the context with data.
  MD5& Update(const void* data, size_t size);

  // A helper function for const char*
  MD5& Update(const char* str) { return Update(str, strlen(str)); }

  // A helper function for const string
  MD5& Update(const std::string& str) { return Update(str.data(), str.size()); }

  // A helper function for int
  MD5& Update(int d) { return Update(&d, sizeof(d)); }

  // The MD5 digest is always 128 bits = 16 bytes
  static const int kDigestLength = 16;

  // Returns the digest as string.
  std::string Digest();

  // A short form of generating MD5 for a string
  std::string operator()(const void* data, size_t size);

  // Converts a binary digest string to a printable string.
  // It is for debugging and unit-test only.
  static std::string DebugString(const std::string& digest);

 private:
  // MD5 context.
  MD5_CTX ctx_;
  // The final MD5 digest.
  unsigned char digest_[kDigestLength];
  // A flag to indicate if MD5_final is called or not.
  bool finalized_;
};

}  // namespace utils
}  // namespace istio

#endif  // ISTIO_UTILS_MD5_H_
