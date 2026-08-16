FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
WORKDIR /app/services/workout
RUN CGO_ENABLED=0 GOOS=linux go build -o /workout-service ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /workout-service /workout-service
COPY services/workout/migrations /migrations
EXPOSE 8081
ENTRYPOINT ["/workout-service"]