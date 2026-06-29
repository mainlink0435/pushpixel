FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o pushpixel ./cmd/pushpixel/

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata

RUN mkdir -p /app/data
WORKDIR /app
COPY --from=builder /app/pushpixel .

EXPOSE 1978
ENTRYPOINT ["./pushpixel", "-config", "/app/data/config.yaml"]
