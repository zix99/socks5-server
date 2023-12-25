ARG GOLANG_VERSION="1.19.1"

FROM golang:$GOLANG_VERSION-alpine as builder
RUN apk --no-cache add tzdata
WORKDIR /go/src/github.com/serjs/socks5
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-s' -o ./socks5

FROM gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.source=https://github.com/zix99/socks5-server
LABEL org.opencontainers.image.description="socks5-server"
LABEL org.opencontainers.image.licenses=MIT

COPY --from=builder /go/src/github.com/serjs/socks5/socks5 /
ENTRYPOINT ["/socks5"]
