FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.Version=${VERSION}" -o /silo ./cmd/silo

FROM gcr.io/distroless/static-debian12
COPY --from=build /silo /usr/local/bin/silo
ENV SILO_DATA_DIR=/data
VOLUME /data
EXPOSE 8082
ENTRYPOINT ["silo", "serve"]
