FROM golang:1-alpine3.22 AS builder

RUN apk add --no-cache git ca-certificates build-base su-exec olm-dev

COPY . /build
WORKDIR /build
RUN go build -o /usr/bin/mautrix-teams

FROM alpine:3.22

ENV UID=1337 \
    GID=1337

RUN apk add --no-cache ffmpeg su-exec ca-certificates olm bash jq curl yq-go lottieconverter

COPY --from=builder /usr/bin/mautrix-teams /usr/bin/mautrix-teams
COPY --from=builder /build/example-config.yaml /opt/mautrix-teams/example-config.yaml
COPY --from=builder /build/docker-run.sh /docker-run.sh
VOLUME /data

CMD ["/docker-run.sh"]
