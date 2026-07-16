# Build stage - compile tau
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /tau ./cmd/tau

# Test stage - run tests in the same environment as CI
FROM golang:1.26-alpine AS test

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go test ./...

# Runtime stage - minimal, distroless-like
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /tau /tau

ENTRYPOINT ["/tau"]
