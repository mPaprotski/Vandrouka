# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /api ./cmd/api
RUN CGO_ENABLED=1 go build -o /migrate ./cmd/migrate
RUN CGO_ENABLED=1 go build -o /import ./cmd/import

FROM python:3.11-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
COPY photo-processing/requirements.txt /opt/photo-processing/requirements.txt
RUN pip install --no-cache-dir --no-deps -r /opt/photo-processing/requirements.txt
COPY photo-processing/process.py /opt/photo-processing/process.py
ENV MEDIA_DEFACE_SCRIPT=/opt/photo-processing/process.py
COPY --from=builder /api /api
COPY --from=builder /migrate /migrate
COPY --from=builder /import /import
COPY migrations /app/migrations
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/api"]
