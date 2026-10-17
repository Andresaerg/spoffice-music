FROM golang:1.26.5-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /smlf ./main.go



FROM alpine:latest
RUN addgroup -S -g 10001 app \
    && adduser -S -u 10001 -G app app \
    && mkdir /data \
    && chown app:app /data
WORKDIR /app
COPY --from=builder /smlf ./smlf
RUN chown app:app ./smlf

ENV TZ=America/Caracas
USER app
VOLUME ["/data"]
EXPOSE 8000

CMD ["./smlf"]
