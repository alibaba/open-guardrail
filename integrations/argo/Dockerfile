# Build from the repository root: docker build -f integrations/argo/Dockerfile .
FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
# Optional build-only transport diagnostics; never persisted into the runtime image.
ARG GODEBUG
COPY api ./api
COPY cmd/guardrailctl ./cmd/guardrailctl
COPY internal ./internal
COPY storage ./storage
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -o /guardrailctl ./cmd/guardrailctl

FROM python:3.12-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends age ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /demo
COPY --from=build /guardrailctl /usr/local/bin/guardrailctl
COPY integrations /demo/integrations
COPY examples/agent-demo/*.json /demo/examples/agent-demo/
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
USER 10001:10001
ENTRYPOINT ["python3", "-m", "integrations.argo.adapter"]
