# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -v -a -installsuffix cgo -o bin/facebook-scraper cmd/scraper/main.go

# Final stage  
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl bash

WORKDIR /app

RUN adduser -D -h /home/scraper -s /bin/bash scraper

COPY --from=builder /app/bin/facebook-scraper .
RUN chmod +x ./facebook-scraper

RUN mkdir -p /app/configs /app/logs /app/data && \
    chown -R scraper:scraper /app

COPY configs/ /app/configs/
RUN chown -R scraper:scraper /app

USER scraper

# Debug command that shows what's happening
CMD ["sh", "-c", "echo 'Container started successfully'; echo 'Contents of /app:'; ls -la /app; echo 'Contents of configs:'; ls -la /app/configs/; echo 'Testing database connection...'; echo 'DB_HOST='$DB_HOST; echo 'DB_USER='$DB_USER; echo 'Starting scraper with debug output...'; ./facebook-scraper -config=configs/config.yaml || (echo 'Scraper failed. Error details:'; cat /app/logs/scraper.log 2>/dev/null || echo 'No log file created'); echo 'Keeping container alive for debugging...'; tail -f /dev/null"]