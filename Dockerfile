# syntax=docker/dockerfile:1
FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o temporal-utility .

FROM gcr.io/distroless/static-debian12

COPY --from=builder /app/temporal-utility /temporal-utility

EXPOSE 9090

ENTRYPOINT ["/temporal-utility"]
