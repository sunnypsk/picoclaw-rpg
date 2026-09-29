# ============================================================
# Stage 1: Build the picoclaw binary
# ============================================================
FROM golang:1.26.8-alpine3.24 AS builder

RUN apk add --no-cache git make

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
ARG VERSION=local
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
RUN make build VERSION="$VERSION" GIT_COMMIT="$GIT_COMMIT" BUILD_TIME="$BUILD_TIME"

# ============================================================
# Stage 2: Standard runtime image
# ============================================================
FROM alpine:3.24
ARG GIT_COMMIT=unknown
LABEL org.opencontainers.image.revision=$GIT_COMMIT

ENV VIRTUAL_ENV=/opt/picoclaw-python
ENV PATH="${VIRTUAL_ENV}/bin:${PATH}"

RUN apk add --no-cache ca-certificates tzdata curl nodejs npm python3 py3-pip && \
    python3 -m venv "${VIRTUAL_ENV}" && \
    "${VIRTUAL_ENV}/bin/pip" install --no-cache-dir \
      gradio_client==2.7.1 \
      openpyxl==3.1.5 \
      pypdf==6.19.0 && \
    ln -sf "${VIRTUAL_ENV}/bin/python" /usr/local/bin/python3 && \
    ln -sf "${VIRTUAL_ENV}/bin/pip" /usr/local/bin/pip3

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:18790/health || exit 1

# Copy binary
COPY --from=builder /src/build/picoclaw /usr/local/bin/picoclaw
COPY docker/entrypoint.sh /usr/local/bin/picoclaw-entrypoint

RUN chmod +x /usr/local/bin/picoclaw-entrypoint

# Create non-root user and group
RUN addgroup -g 1000 picoclaw && \
    adduser -D -u 1000 -G picoclaw picoclaw

# Switch to non-root user
USER picoclaw

# Run onboard to create initial directories and config
RUN /usr/local/bin/picoclaw onboard

ENTRYPOINT ["/usr/local/bin/picoclaw-entrypoint"]
CMD ["gateway"]
