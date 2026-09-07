FROM golang:1.26-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGET=server

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/${TARGET} \
        ./cmd/${TARGET}


FROM alpine:3.22 AS server

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/server /app/server

USER nobody:nobody

ENTRYPOINT ["/app/server"]


FROM alpine:3.22 AS worker

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/worker /app/worker

USER nobody:nobody

ENTRYPOINT ["/app/worker"]