# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=golang:1.26.2-alpine3.23@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1
ARG RUNTIME_IMAGE=alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

FROM ${GO_IMAGE} AS build

ARG POMEGRANATE_REF
RUN apk add --no-cache ca-certificates=20260611-r0

WORKDIR /src
COPY vendor/pomegranate.lock.json /tmp/pomegranate.lock.json
COPY vendor/pomegranate.sha256 /tmp/pomegranate.sha256
COPY vendor/pomegranate/ ./

RUN case "${POMEGRANATE_REF}" in \
      *[!0-9a-f]*|'') echo 'POMEGRANATE_REF must be a lowercase 40-character commit SHA' >&2; exit 64 ;; \
    esac && \
    test "${#POMEGRANATE_REF}" -eq 40 && \
    grep -Fq "\"commit\": \"${POMEGRANATE_REF}\"" /tmp/pomegranate.lock.json && \
    sha256sum -c /tmp/pomegranate.sha256 && \
    grep -Fxq 'go 1.26.2' go.mod && \
    grep -Fq 'fiatjaf.com/promenade v0.4.4-0.20260511003220-ce69ab3c4a4d' go.mod && \
    grep -Fq 'github.com/a-h/templ v0.3.1020' go.mod

RUN go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate && \
    test -z "$(find . -name '*.go' -type f -exec gofmt -l {} +)" && \
    go test ./... && \
    mkdir -p /out && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -buildid=' \
      -o /out/pomegranate-central ./central && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -buildid=' \
      -o /out/pomegranate-operator ./operator

FROM scratch AS binaries

COPY --from=build /out/ /

FROM ${RUNTIME_IMAGE} AS runtime

ARG POMEGRANATE_REF

LABEL org.opencontainers.image.source="https://basspistol.org/npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6/pomegranate.git" \
      org.opencontainers.image.revision="${POMEGRANATE_REF}" \
      org.opencontainers.image.licenses="MIT AND Unlicense"

RUN apk add --no-cache ca-certificates=20260611-r0 socat=1.8.1.3-r0 && \
    addgroup -g 65532 -S pomegranate && \
    adduser -u 65532 -S -D -H -G pomegranate pomegranate && \
    install -d -o 65532 -g 65532 -m 0700 /var/lib/pomegranate

COPY --from=build /out/pomegranate-central /usr/local/bin/pomegranate-central
COPY --from=build /out/pomegranate-operator /usr/local/bin/pomegranate-operator
COPY docker/entrypoint.sh /usr/local/bin/pomegranate-entrypoint
COPY docker/healthcheck.sh /usr/local/bin/pomegranate-healthcheck
COPY docker/log-filter.awk /usr/local/share/pomegranate/log-filter.awk

RUN chmod 0555 /usr/local/bin/pomegranate-central \
    /usr/local/bin/pomegranate-operator \
    /usr/local/bin/pomegranate-entrypoint \
    /usr/local/bin/pomegranate-healthcheck && \
    chmod 0444 /usr/local/share/pomegranate/log-filter.awk

USER 65532:65532
WORKDIR /var/lib/pomegranate
EXPOSE 5033 5041
ENTRYPOINT ["/usr/local/bin/pomegranate-entrypoint"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD ["/usr/local/bin/pomegranate-healthcheck"]
