FROM docker.io/library/golang:1.24.5-alpine AS builder
LABEL "language"="go"
RUN mkdir /src
WORKDIR /src

COPY . /src/
RUN go mod download
ENV CGO_ENABLED=0

RUN go build -o ./bin/server ./cmd/server/
FROM alpine AS runtime
COPY --from=builder /src/bin/server /bin/server
CMD ["/bin/server"]
