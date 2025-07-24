# Stage 1: Build the Go application
FROM golang:1.24-alpine AS builder

# Install build tools and SQLite dev libraries for CGO
RUN apk add --no-cache build-base sqlite-dev

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Ensure templates are up-to-date using 'go run'
RUN go run github.com/a-h/templ/cmd/templ@latest generate

# Build the Go application with CGO enabled
RUN go build -o /dokeep ./cmd/dokeep

# Stage 2: Create the final, minimal image
FROM alpine:latest

# The go-sqlite3 driver requires sqlite libs at runtime
RUN apk --no-cache add ca-certificates sqlite-libs

WORKDIR /app/

# Copy the binary from the builder stage
COPY --from=builder /dokeep .

# Expose port 8081 to the outside world
EXPOSE 8081

# Command to run the executable
CMD ["./dokeep"] 