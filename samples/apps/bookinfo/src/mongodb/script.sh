#! /bin/sh 
set -e
echo $PWD
mongoimport --host localhost --db test --collection ratings --drop --file /app/data/ratings_data.json
