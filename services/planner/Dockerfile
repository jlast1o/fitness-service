FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
WORKDIR /app/services/planner
RUN CGO_ENABLED=0 GOOS=linux go build -o /planner-service ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /planner-service /planner-service
COPY services/planner/migrations /migrations
EXPOSE 8083
ENTRYPOINT ["/planner-service"]