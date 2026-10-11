#!/bin/zsh

BINARY=joyWaApi
BINDIR=bin

# JoyWaApi.dll が 32bit なので、どの OS でも Windows の 32bit 向けにビルドする
build:
	GOOS=windows CGO_ENABLED=0 GOARCH=386 go build -tags timetzdata,server -o ${BINDIR}/${BINARY}.exe app/*.go

# GraphQL のスキーマ（graph/schema.graphqls）を変えたら実行する
generate:
	go tool gqlgen generate
