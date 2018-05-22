#!/bin/sh
export GOPATH=/go/src
cd /go/src
go get "github.com/nu7hatch/gouuid"
go get -u "github.com/lib/pq"
go get  "github.com/go-martini/martini"
go get  "github.com/martini-contrib/binding"
go get  "github.com/martini-contrib/render"
go get  "github.com/akkeris/vault-client"
go get  "github.com/aws/aws-sdk-go/aws"
go get  "github.com/aws/aws-sdk-go/aws/session"
go get  "github.com/aws/aws-sdk-go/service/elasticsearchservice"

cd /go/src/es-aws-api
go build server.go

