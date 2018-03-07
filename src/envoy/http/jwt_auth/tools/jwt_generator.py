#!/usr/bin/env python
#
# Copyright 2018 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import argparse
import sys
import jwt  # pip install PyJWT and pip install cryptography.

""" This script is used to generate ES256/RS256-signed jwt token."""

"""commands to generate private_key_file:
  ES256: $ openssl ecparam -genkey -name prime256v1 -noout -out private_key.pem
  RS256: $ openssl genpkey -algorithm RSA -out private_key.pem -pkeyopt rsa_keygen_bits:2048
"""

def main(args):
  # JWT token generation.
  with open(args.private_key_file, 'r') as f:
    try:
      secret = f.read()
    except:
      print("Failed to load private key.")
      sys.exit()

  # Token headers
  hdrs = {'alg': args.alg,
          'typ': 'JWT'}
  if args.kid:
    hdrs['kid'] = args.kid

  # Token claims
  claims = {'iss': args.iss,
            'sub': args.sub,
            'aud': args.aud}
  if args.email:
    claims['email'] = args.email
  if args.azp:
    claims['azp'] = args.azp
  if args.exp:
    claims['exp'] = args.exp

  # Change claim and headers field to fit needs.
  jwt_token = jwt.encode(claims,
                         secret,
                         algorithm=args.alg,
                         headers=hdrs)

  print(args.alg + "-signed jwt:")
  print(jwt_token)


if __name__ == '__main__':
  parser = argparse.ArgumentParser(
      description=__doc__,
      formatter_class=argparse.RawDescriptionHelpFormatter)

  # positional arguments
  parser.add_argument(
      "alg",
      help="Signing algorithm, i.e., ES256/RS256.")
  parser.add_argument(
      "iss",
      help="Token issuer, which is also used for sub claim.")
  parser.add_argument(
      "aud",
      help="Audience. This must match 'audience' in the security configuration"
      " in the swagger spec.")
  parser.add_argument(
      "private_key_file",
      help="The path to the generated ES256/RS256 private key file, e.g., /path/to/private_key.pem.")

  #optional arguments
  parser.add_argument("-e", "--email", help="Preferred e-mail address.")
  parser.add_argument("-a", "--azp", help="Authorized party - the party to which the ID Token was issued.")
  parser.add_argument("-x", "--exp", type=int, help="Token expiration claim.")
  parser.add_argument("-k", "--kid", help="Key id.")
  parser.add_argument("-s", "--sub", help="Token subject claim.")
  main(parser.parse_args())
