FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
WORKDIR /app/services/auth
RUN CGO_ENABLED=0 GOOS=linux go build -o /auth-service ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /auth-service /auth-service
COPY services/auth/migrations /migrations
EXPOSE 8080
ENTRYPOINT ["/auth-service"]