FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/csx-3323730970/gocr/internal/cli.version=${VERSION}" \
    -o /out/gocr ./cmd/gocr

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=build /out/gocr /usr/local/bin/gocr
USER app
EXPOSE 8080
ENTRYPOINT ["gocr"]
CMD ["serve"]
