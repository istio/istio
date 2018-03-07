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
import chilkat   #  Chilkat v9.5.0.66 or later.

""" This script is used to generate ES256/RS256 public jwk key."""

"""commands to generate public_key_file (Note that private key needs to be generated first):
  ES256: $ openssl ecparam -genkey -name prime256v1 -noout -out private_key.pem
         $ openssl ec -in private_key.pem -pubout -out public_key.pem
  RS256: $ openssl genpkey -algorithm RSA -out private_key.pem -pkeyopt rsa_keygen_bits:2048
         $ openssl rsa -pubout -in private_key.pem -out public_key.pem
"""

def main(args):
  #  Load public key file into memory.
  sbPem = chilkat.CkStringBuilder()
  success = sbPem.LoadFile(args.public_key_file, "utf-8")
  if (success != True):
    print("Failed to load public key.")
    sys.exit()

  #  Load the key file into a public key object.
  pubKey = chilkat.CkPublicKey()
  success = pubKey.LoadFromString(sbPem.getAsString())
  if (success != True):
    print(pubKey.lastErrorText())
    sys.exit()

  #  Get the public key in JWK format:
  jwk = pubKey.getJwk()
  # Convert it to json format.
  json = chilkat.CkJsonObject()
  json.Load(jwk)
  # This line is used to set output format.
  cpt = True
  if (not args.compact) or (args.compact and args.compact == "no"):
    cpt = False
  json.put_EmitCompact(cpt)
  # Additional information can be added like this. change to fit needs.
  if args.alg:
    json.AppendString("alg", args.alg)
  if args.kid:
    json.AppendString("kid", args.kid)
  # Print.
  print("Generated " + args.alg + " public jwk:")
  print(json.emit())


if __name__ == '__main__':
  parser = argparse.ArgumentParser(
      description=__doc__,
      formatter_class=argparse.RawDescriptionHelpFormatter)

  # positional arguments
  parser.add_argument(
      "alg",
      help="Signing algorithm, e.g., ES256/RS256.")
  parser.add_argument(
      "public_key_file",
      help="The path to the generated ES256/RS256 public key file, e.g., /path/to/public_key.pem.")

  #optional arguments
  parser.add_argument("-c", "--compact", help="If making json output compact, say 'yes' or 'no'.")
  parser.add_argument("-k", "--kid", help="Key id, same as the kid in private key if any.")
  main(parser.parse_args())