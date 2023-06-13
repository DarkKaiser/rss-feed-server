# ------------------------------------------
# 1. Build Image
# ------------------------------------------
FROM golang:1.20.5-bullseye AS builder

ARG APP_NAME=rss-feed-server

WORKDIR /go/src/app/

COPY . .

ENV GO111MODULE=on

RUN CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -a -ldflags="-s -w" -o ${APP_NAME} .

# ------------------------------------------
# 2. Production Image
# ------------------------------------------
FROM debian:bullseye

ARG APP_NAME=rss-feed-server

COPY docker-entrypoint.sh /docker-entrypoint/
RUN chmod +x /docker-entrypoint/docker-entrypoint.sh

WORKDIR /docker-entrypoint/dist/

COPY --from=builder /go/src/app/${APP_NAME} .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY ./secrets/${APP_NAME}.운영.json /docker-entrypoint/dist/${APP_NAME}.json

WORKDIR /usr/local/app/

ENTRYPOINT ["/docker-entrypoint/docker-entrypoint.sh"]
CMD ["/usr/local/app/rss-feed-server"]
