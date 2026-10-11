#!/bin/zsh
cd `dirname $0`

#protoc --go_out=. --go_opt=paths=source_relative \
#--go-grpc_out=. --go-grpc_opt=paths=source_relative \
#./joyWaApi.proto

protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ./joyWaApi.proto




