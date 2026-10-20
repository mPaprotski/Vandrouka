# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
