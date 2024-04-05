FROM golang:1.22.1-bullseye as build

ENV CGO_ENABLED=1
ENV GOOS=linux

WORKDIR /build
COPY . .

RUN go mod download
RUN go mod verify

RUN go build -ldflags='-s -w -extldflags "-static"' -o main .

FROM gcr.io/distroless/static-debian11

WORKDIR /openfsd

COPY --from=build /build/main .

CMD ["./main"]