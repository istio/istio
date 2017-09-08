#! /bin/sh 
set -e
mongoimport --host localhost --db test --collection ratings --drop --file /app/data/ratings_data.json
