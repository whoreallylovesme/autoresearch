FROM golang@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/researchd ./cmd/researchd \
    && CGO_ENABLED=0 go build -trimpath -o /out/researchctl ./cmd/researchctl

FROM python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b AS runtime
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    UV_CACHE_DIR=/tmp/uv-cache \
    HOME=/tmp
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl poppler-utils \
    && rm -rf /var/lib/apt/lists/*
COPY --from=ghcr.io/astral-sh/uv@sha256:a3ea1357342c1a4bf9a0c53a3deb1b55379aefa037338d8113d5771efa01774f /uv /usr/local/bin/uv
WORKDIR /opt/autoresearch
COPY . .
RUN uv sync --frozen --no-dev --project web \
    && mkdir -p /var/lib/autoresearch /tmp/uv-cache \
    && chown -R 65532:65532 /opt/autoresearch /var/lib/autoresearch /tmp/uv-cache
COPY --from=build /out/researchd /out/researchctl /opt/autoresearch/bin/
USER 65532:65532
EXPOSE 8080 8081
CMD ["./bin/researchd"]
