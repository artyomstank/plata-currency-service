FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/ ./cmd/service ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot AS migrate
COPY --from=build /out/migrate /migrate
ENTRYPOINT ["/migrate"]

FROM gcr.io/distroless/static-debian12:nonroot AS currency-service
COPY --from=build /out/service /currency-service
ENTRYPOINT ["/currency-service"]
