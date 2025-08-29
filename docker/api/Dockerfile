FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go/api/go.mod go/api/go.sum ./

RUN apk add --no-cache git ca-certificates

RUN go env -w GOPROXY=https://proxy.golang.org && go mod download

COPY go/api/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/api ./main.go

FROM alpine:3.18
RUN apk add --no-cache ca-certificates netcat-openbsd && \
    adduser -D -s /bin/sh apiuser

COPY --from=build /bin/api /bin/api
RUN chmod +x /bin/api

USER apiuser
EXPOSE 8080
ENTRYPOINT ["/bin/api"]
