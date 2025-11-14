FROM golang:1.24-alpine AS build
WORKDIR /src

# Copy pkg directory first
COPY go/pkg /src/pkg

# Copy worker module files
COPY go/worker/go.mod go/worker/go.sum ./

RUN apk add --no-cache git ca-certificates
RUN go env -w GOPROXY=https://proxy.golang.org && go mod download
COPY go/worker/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/worker ./main.go
FROM alpine:3.18
RUN apk add --no-cache ca-certificates && adduser -D -s /bin/sh workeruser

COPY --from=build /bin/worker /bin/worker
RUN chmod +x /bin/worker
USER workeruser
ENTRYPOINT ["/bin/worker"]
