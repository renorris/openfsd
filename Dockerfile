FROM golang:1.25 AS build

WORKDIR /go/src/openfsd
COPY go.mod go.sum ./

# Cache module downloads
RUN go mod download

COPY . .

# Cache builds — single binary with both FSD and web services
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build" \
    CGO_ENABLED=0 go build -o /go/bin/openfsd ./cmd/openfsd

FROM alpine:latest

LABEL org.opencontainers.image.source=https://github.com/renorris/openfsd

RUN addgroup -g 2001 nonroot && \
    adduser -u 2001 -G nonroot -D nonroot && \
    mkdir /db && chown -R nonroot:nonroot /db

COPY --from=build --chown=nonroot:nonroot /go/bin/openfsd /

USER 2001:2001

# Default: run both FSD and web. Override CMD to pass e.g. only -fsd or only -web.
EXPOSE 6809 8000
CMD ["/openfsd"]
