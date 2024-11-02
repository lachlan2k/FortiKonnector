# Builder
FROM golang:1.22 AS builder

WORKDIR /app

COPY go.sum go.mod .
RUN go mod download -x
COPY . .
RUN go build -o fortikonnector .

# Runtime
FROM redhat/ubi9-minimal AS runtime

WORKDIR /app
COPY --from=builder /app/fortikonnector .

ENTRYPOINT ["/app/fortikonnector"]