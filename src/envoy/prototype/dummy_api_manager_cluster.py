from BaseHTTPServer import BaseHTTPRequestHandler
from BaseHTTPServer import HTTPServer
from subprocess import Popen, PIPE, STDOUT

PUBLIC_KEY = '''{
 "keys": [
  {
   "kty": "RSA",
   "alg": "RS256",
   "use": "sig",
   "kid": "686f442dead739e241342519849bd0d140707a86",
   "n": "yVGRzBnVmCHC9uMH_mCna7FiSKOjMEsDdgf8hds41KYfUHH9cp4P41iQEBnqPYvQiMYaZDuatkMDh25ukmkSyfLwJIVQpnHgMDwoZ1cx83Z0PwC8wAAJYqKyItKNfFN4UJ6DZIOVU-Iqgxi8VOtGwMNx2LiD1NoFVfXyz52UJ_QLiUGzErVwTGv4UD6NtaWKFkctTnEG-9rZvDF8QknnzxAomVa2OcV8OHeszx6N8UE1tm9Kq4xj2Uu8D3dDrfu2jr45Pi6RHIZOTAnu8G7wTDNaiGbENrbHSk6WAjLZBOcWZj9SDlDlwH2xFoKdpNKRmLKpPQblHem_y1KRYwvJTw",
   "e": "AQAB"
  },
  {
   "kty": "RSA",
   "alg": "RS256",
   "use": "sig",
   "kid": "d57d66bfbff089769e3545589572d3b53955a8cb",
   "n": "zX99YCqicbn_j-5YTJ-2FsgONUg7cmqJiwvHXrUopRvn2Tukrd0B5Sg-Rq1hdZTYgIym7lSMw_zLxIQCH54sUfydX5MMWr6FOxVUbYl-E0Oko85Yer9dFv61rN0USj_A12QRMmjCZkcqH_6MWWuA1QWaejyStopjpLEYnUD3bP6oS604eZWkkOp8Nu-Vg4NqqX7ZClIcQqe03xv3sFHiPuhB-qaifhpIPpKCiYSKxAY4_GxwCJ_ml_uJ5k1tIrDykAlie6aWxv8hogOXrQmNRCO2Qcumwb7d9cci1UxsEYOtpZxhTiZqWsbrBxwfvLqU_rvsCT8vOjrBPOkTtYl22w",
   "e": "AQAB"
  },
  {
   "kty": "RSA",
   "alg": "RS256",
   "use": "sig",
   "kid": "516ca9a83b130d4b605684fe7df83b43f33fba43",
   "n": "rSLxCvtRLoJUt2xPTSjDgN8_7kuBLsZz0JD4HRiwvId_IYY-T5MsUirBx01AtI3KRlmeQ-1QIvET4cNN99QL8U6e7Hh75VWJvQhh0_TFdKbv_75b81-rzx-EvIoVe0czeY3QeRp3DDORRk9o7xgri6U3VfT2zcQIDAPY14h3wLHE5DQuH9EpbxjS_wNrQxRYZH_mWgmWU7h001WHWRDKdEIgtzQkinKOTig90Wy4pt_vgMGcv0mh_wYvIcmB62Qj1sHUUOa5NJWUaZLC4Stj7CH0dLiBrVqg3JFqvO60Oo3wrcdmNQl0RaRHFjBYLameUPSP6M2BevfCRx8-Ix26rw",
   "e": "AQAB"
  },
  {
   "kty": "RSA",
   "alg": "RS256",
   "use": "sig",
   "kid": "33a681b36b8983913b05f8d5eab165c60694b6fc",
   "n": "pRbWhLXIOvF1Naxh5n2evZqpJ3kPEFL4b-jHRHOhMnzqsi_aeQtIwYLVM8zdYYfgCxRq8umG1YwMenBKKPzRWr4MkFAB64O3UPgdyg-3je-fgCMziqS_3KH7AXHekxG_ZHpwbkgilRMtJNiDnSZWGad8XAfW3VFct4RqRAaf7h-6Za0IM7R3u4VYkUfosNqKtoDJDQrng9Nbv9ryUk8u1WikKF0M1r-wrZoDA7QFRAFkbdfYyvHL2daUflNDIXmFkUeHGGApMlJQ3wJk7Tln4txGMSUdJoD3JWEyKa2W1WshtqBHnQb8VlL77H-ch9zbn5pGZCoJ0MsgJr0vjoea8w",
   "e": "AQAB"
  },
  {
   "kty": "RSA",
   "alg": "RS256",
   "use": "sig",
   "kid": "986f2c30b9843defdc8f8debe138d640d1d67d9d",
   "n": "qfE-u8I60zP_Vg3UsP_X9Qv1idkrnsXKkcQcsGWUdqs4jM2VixGBJDMJWbP55aJA93Sl49_dzAv_lr5-ctc1L04ke6nHX19EEBDtFTjGKyEdI-X1C-6HXCQmpI1XvUA8DzTOIZd5KEXJgBA9tpn6qkMAHzXyMHOfg8nhW36She4QggFjfF_RDKOA2-jRXDIjinOmIaLhVq4hsC9hCshhsfreLPw3HH8UhRONbkoFU_8ZBgAQLdVB1TQwp-ZDiVyh9od9a-RmfIiUl27AK5LDrtpVRCtXj6bv9OMx4QWVX8G-NfQexDwAp1pICaE5qYyJbiIK25E07vPTJ1ZtMpodFQ",
   "e": "AQAB"
  }
 ]
}
'''

def print_body(binary, expected):
  p = Popen(['bazel-bin/src/tools/service_control_json_gen',
          '--text',
          '--'+expected,
          '--stdin'], stdin=PIPE)
  p.communicate(input=binary)

class Handler(BaseHTTPRequestHandler):
  def do_GET(self):
    self.handle_request("GET")

  def do_POST(self):
    self.handle_request("POST")



  def handle_request(self, method):
    print(method, self.path, self.headers.items())
    url = self.headers.get('x-api-manager-url', '')
    if url == 'https://www.googleapis.com/service_accounts/v1/jwk/loadtest@esp-test-client.iam.gserviceaccount.com':
      self.send_response(200)
      self.send_header('Content-Length', str(len(PUBLIC_KEY)))
      self.end_headers()
      self.wfile.write(PUBLIC_KEY)
    else:
      #content_len = self.headers.getheader('content-length', 0)
      #post_body = self.rfile.read(int(content_len))
      if url.endswith(":report"):
        print_body(post_body, "report_request")
      elif url.endswith(":check"):
        print_body(post_body, "check_request")

      self.send_response(200)
      self.send_header('Content-Length', "0")
      self.end_headers()


if __name__ == '__main__':
  server = HTTPServer(('localhost', 8081), Handler)
  print 'Starting server, use <Ctrl-C> to stop'
  server.serve_forever()
