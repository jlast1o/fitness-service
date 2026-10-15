FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

WORKDIR /app/services/nutrition

RUN CGO_ENABLED=0 GOOS=linux go build -o /nutrition-service ./cmd/server


FROM alpine:3.19

RUN apk --no-cache add ca-certificates

COPY --from=builder /nutrition-service /nutrition-service
COPY services/nutrition/migrations /migrations

EXPOSE 8084

ENTRYPOINT ["/nutrition-service"]