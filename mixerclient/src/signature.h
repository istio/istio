/* Copyright 2017 Google Inc. All Rights Reserved.
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

// Utility functions used to generate signature for operations, metric values,
// and check requests.

#ifndef MIXER_CLIENT_SIGNATURE_H_
#define MIXER_CLIENT_SIGNATURE_H_

#include <string>
#include "include/client.h"

namespace istio {
namespace mixer_client {

// Generates signature for Attributes.
std::string GenerateSignature(const Attributes& attributes);

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXER_CLIENT_SIGNATURE_H_
