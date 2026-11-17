# golang:1.27.3-bookworm, matches the go version pinned in mise.toml
FROM golang@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS builder

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
