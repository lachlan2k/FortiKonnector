# Builder
FROM golang:1.21 AS builder

WORKDIR /app

COPY . .
RUN go build -o fortikonnector .

# Runtime
FROM redhat/ubi9-minimal AS runtime

WORKDIR /app
COPY --from=builder /app/fortikonnector .

ENTRYPOINT ["/app/fortikonnector"]