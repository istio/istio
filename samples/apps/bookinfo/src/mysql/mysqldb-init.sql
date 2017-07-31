/*
 * Initialize a mysql db with a 'test' db and be able test productpage with it.
 * mysql -h 127.0.0.1 -ppassword < mysqldb-init.sql
 */
USE test;
CREATE TABLE price (name VARCHAR(20), price INT);
INSERT INTO price VALUES('comedy', 20);
SELECT * FROM price;
