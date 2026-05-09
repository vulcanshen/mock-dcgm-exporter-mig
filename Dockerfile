FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /mock-dcgm-exporter-mig .

FROM scratch
COPY --from=builder /mock-dcgm-exporter-mig /mock-dcgm-exporter-mig
COPY config.yaml /config.yaml
EXPOSE 9400
ENTRYPOINT ["/mock-dcgm-exporter-mig", "-config", "/config.yaml"]
