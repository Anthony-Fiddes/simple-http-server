#!/bin/sh
go run . localhost:4221 --directory . &
SERVER_PID=$!
SEPARATOR="\n\n=========================\n\n"

# Get an idea for some basic stuff the server can do

echo "Simply echo back a path argument"
echo
curl -vvv -X GET http://localhost:4221/echo/mango

echo $SEPARATOR
echo "Serve files from a directory"
echo
curl -vvv -X GET http://localhost:4221/files/README.md

echo $SEPARATOR
echo "Either of the above can be gzip encoded (note that curl will not print binary output to your terminal)"
echo
curl -vvv -X GET http://localhost:4221/files/README.md -H "Accept-Encoding: gzip"

kill $SERVER_PID
