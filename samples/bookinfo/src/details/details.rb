#!/usr/bin/ruby
#
# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

require 'webrick'
require 'json'
require 'net/http'

if ARGV.length < 1 then
    puts "usage: #{$PROGRAM_NAME} port"
    exit(-1)
end

port = Integer(ARGV[0])

server = WEBrick::HTTPServer.new :BindAddress => '0.0.0.0', :Port => port

trap 'INT' do server.shutdown end

server.mount_proc '/health' do |req, res|
    res.status = 200
    res.body = {'status' => 'Details is healthy'}.to_json
    res['Content-Type'] = 'application/json'
end

server.mount_proc '/details' do |req, res|
    pathParts = req.path.split('/')
    begin
        begin
          id = Integer(pathParts[-1])
        rescue
          raise 'please provide numeric product id'
        end
        details = get_book_details(id)
        res.body = details.to_json
        res['Content-Type'] = 'application/json'
    rescue => error
        res.body = {'error' => error}.to_json
        res['Content-Type'] = 'application/json'
        res.status = 400
    end
end

# TODO: provide details on different books.
def get_book_details(id)
    if ENV['ENABLE_EXTERNAL_BOOK_SERVICE'] === 'true' then
        # the ISBN of the first book Comedy Of Errors that appears in Amazon.com search
        isbn = '1420955551'
        return fetch_details_from_external_service(isbn, id)
    end

    return {
        'id' => id,
        'author': 'William Shakespeare',
        'year': 1595,
        'type' => 'paperback',
        'pages' => 200,
        'publisher' => 'PublisherA',
        'language' => 'English',
        'ISBN-10' => '1234567890',
        'ISBN-13' => '123-1234567890'
    }
end

def fetch_details_from_external_service(isbn, id)
    uri = URI.parse('http://api.bookmooch.com')

    response = Net::HTTP.start(uri.host, uri.port) do |http|
        http.read_timeout = 5 # seconds
        http.get('/api/asin?asins=' + isbn  + '&o=json')
    end

    json = JSON.parse(response.body)
    book = json[0]

    return {
        'id' => id,
        'author': book['Author'],
        'year': book['PublicationDate'],
        'type' => book['Binding'],
        'pages' => book['NumberOfPages'],
        'publisher' => book['Publisher'],
        'language' => 'English', # hardcoded, not returned by bookmooch.com
        'ISBN-10' => book['ISBN'],
        'ISBN-13' => '978-1420955552' # hardcoded, not returned by bookmooch.com
  }

end

server.start
