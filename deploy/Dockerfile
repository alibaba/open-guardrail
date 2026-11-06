# Keep this default synchronized with the go directive in go.mod. The test target
# enforces the relationship because Docker cannot interpolate values from go.mod
# in a FROM instruction.
ARG GO_VERSION=1.27.3
FROM golang:${GO_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=open-guardrail-go-mod,target=/go/pkg/mod,sharing=locked \
    GOWORK=off go mod download
COPY . .
RUN --mount=type=cache,id=open-guardrail-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=open-guardrail-go-build,target=/root/.cache/go-build,sharing=locked \
    GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/open-guardrail ./cmd/open-guardrail && \
    GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/guardrailctl ./cmd/guardrailctl

# Seed a fresh named volume with private ownership for the runtime UID/GID.
RUN mkdir /out/data && chown 65532:65532 /out/data && chmod 0700 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
USER 65532:65532
COPY --from=build --chown=65532:65532 /out/data /data
WORKDIR /data
ENV OPEN_GUARDRAIL_DATA_DIR=/data \
    OPEN_GUARDRAIL_LISTEN=0.0.0.0:8080
COPY --from=build /out/open-guardrail /usr/local/bin/open-guardrail
COPY --from=build /out/guardrailctl /usr/local/bin/guardrailctl
COPY LICENSE NOTICE /usr/share/licenses/open-guardrail/
COPY third_party /usr/share/licenses/open-guardrail/third_party
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/open-guardrail"]
# The external HTTPS origin is deliberately supplied by the deployment.
CMD ["serve"]
