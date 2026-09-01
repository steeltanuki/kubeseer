# syntax=docker/dockerfile:1

FROM golang:1.26.7 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=unknown
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT} -X main.buildDate=${BUILD_DATE}" -o /out/kubeseer ./cmd/kubeseer

FROM scratch

ARG VERSION=unknown
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="Kubeseer" \
      org.opencontainers.image.description="Kubeseer Kubernetes observation controller" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/steeltanuki/kubeseer" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=build /out/kubeseer /kubeseer
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

USER 65532:65532
ENTRYPOINT ["/kubeseer", "manager"]
