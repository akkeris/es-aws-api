FROM golang:1.10-alpine
RUN apk update
RUN apk add openssl ca-certificates git
RUN mkdir -p /go/src/es-aws-api
WORKDIR /go/src/es-aws-api
COPY . .
WORKDIR /
ADD build.sh /build.sh
RUN chmod +x /build.sh
RUN /build.sh
WORKDIR /go/src/es-aws-api
CMD ["/go/src/es-aws-api/server"]
EXPOSE 9000

