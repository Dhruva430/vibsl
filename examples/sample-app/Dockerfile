# Multi-stage build producing a tiny, static, non-root image. The runtime
# stage is distroless/static with the "nonroot" user so it satisfies the
# hardened Deployment (runAsNonRoot + readOnlyRootFilesystem) the API generates.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
# CGO disabled => fully static binary that runs on distroless/static.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/app .

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
