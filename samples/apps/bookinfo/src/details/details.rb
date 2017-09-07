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
        id = Integer(pathParts[-1])
        details = get_book_details(id)
        res.body = details.to_json
        res['Content-Type'] = 'application/json'
    rescue
        res.body = {'error' => 'please provide numeric product id'}.to_json
        res['Content-Type'] = 'application/json'
        res.status = 400
    end
end

# TODO: provide details on different books.
def get_book_details(id)
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

def get_book_deatils_html(id)
    details = get_book_details(id)
    html = '<dl>'
    details.each do |k, v|
        html += '<dt>%s:</dt>%s' % [k.capitalize, v]
    end
    html += '</dl>'
    return html
end

server.mount_proc '/' do |req, res|
  res.body = '
    <html>
    <head>
    <meta charset="utf-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1">

    <!-- Latest compiled and minified CSS -->
    <link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.5/css/bootstrap.min.css">

    <!-- Optional theme -->
    <link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.5/css/bootstrap-theme.min.css">

    <!-- Latest compiled and minified JavaScript -->
    <script src="https://ajax.googleapis.com/ajax/libs/jquery/2.1.4/jquery.min.js"></script>

    <!-- Latest compiled and minified JavaScript -->
    <script src="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.5/js/bootstrap.min.js"></script>

    </head>
    <title>Book details service</title>
    <body>
    <p><h2>Hello! This is the book details service. My content is</h2></p>
    <div>
    <h4 class="text-center text-primary">Book Details</h4>
    %s
    </div>
    </body>
    </html>
  ' % [get_book_deatils_html(1)]
  res['Content-Type'] = 'text/html'
end

server.start
