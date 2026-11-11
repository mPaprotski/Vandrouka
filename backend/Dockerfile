# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /api ./cmd/api
RUN CGO_ENABLED=1 go build -o /migrate ./cmd/migrate
RUN CGO_ENABLED=1 go build -o /import ./cmd/import

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /api /api
COPY --from=builder /migrate /migrate
COPY --from=builder /import /import
COPY migrations /app/migrations
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/api"]
