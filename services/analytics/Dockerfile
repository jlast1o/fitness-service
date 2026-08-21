FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
WORKDIR /app/services/analytics
RUN CGO_ENABLED=0 GOOS=linux go build -o /analytics-service ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /analytics-service /analytics-service
COPY services/analytics/migrations /migrations
EXPOSE 8082
ENTRYPOINT ["/analytics-service"]