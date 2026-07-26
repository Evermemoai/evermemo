# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /evermemo .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /evermemo /evermemo
ENV EVERMEMO_DB=/data/evermemo.db
VOLUME /data
EXPOSE 7777
ENTRYPOINT ["/evermemo"]
CMD ["serve", "--addr", ":7777"]
