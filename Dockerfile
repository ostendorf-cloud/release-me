# golang:1.27.3-bookworm, matches the go version pinned in mise.toml
FROM golang@sha256:ec47ea3725fea0cc88f6de94919aef19f40f12fc1ec0e2d1b3426dbf15ecca04 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=${VERSION}" -o /release-me ./cmd/release-me

# gcr.io/distroless/static:nonroot, runs as the image's built in "nonroot"
# user (65532:65532), so no USER instruction is needed here.
FROM gcr.io/distroless/static@sha256:1a64f46364620465ef9e9a8f5413757dfdf22695e050bbfd7303eeaf17a59383

COPY --from=builder /release-me /release-me

ENTRYPOINT ["/release-me"]
