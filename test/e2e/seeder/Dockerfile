# Built from the repository root: docker build -f test/e2e/seeder/Dockerfile .
FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /seeder ./test/e2e/seeder && mkdir -p /out/shared /out/seed

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /seeder /seeder
COPY --from=build --chown=65532:65532 /out/shared /shared
COPY --from=build --chown=65532:65532 /out/seed /seed
ENTRYPOINT ["/seeder"]
