FROM golang:1.26-alpine AS build
WORKDIR /src
# ca-certificates so the scratch stage can verify TLS to ghcr.io,
# helm-chart repos, OCI registries, etc.
RUN apk add --no-cache ca-certificates upx
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fluxrr ./cmd/fluxrr
RUN upx --best --lzma /out/fluxrr

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/fluxrr /fluxrr
ENTRYPOINT ["/fluxrr"]
