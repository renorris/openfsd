# Fetch UPX
FROM alpine:latest AS upx
WORKDIR /
RUN apk update && apk add ca-certificates && \
    arch=$(arch | sed s/aarch64/arm64/ | sed s/x86_64/amd64/) && echo "ARCHITECTURE=${arch}" && \
    wget "https://github.com/upx/upx/releases/download/v4.2.4/upx-4.2.4-${arch}_linux.tar.xz" && \
    tar -xf upx-4.2.4-${arch}_linux.tar.xz &&  \
    cd upx-4.2.4-${arch}_linux && \
    mv upx /bin/upx

# Build openfsd
FROM golang:1.23.2-bookworm AS build

WORKDIR /go/src/openfsd

# Add the sources
COPY . .

# Download modules
RUN go mod download

# Compile
RUN mkdir -p build && CGO_ENABLED=0 GOOS=linux go build -v -o build/openfsd -ldflags "-s -w" main.go

# Move UPX into /bin
COPY --from=upx /bin/upx /bin/upx

# Compress with upx
RUN upx -v -9 build/openfsd

# Final distroless image
FROM gcr.io/distroless/static-debian12

USER nonroot:nonroot

WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /go/src/openfsd/build /app

ENTRYPOINT ["/app/openfsd"]
