# Multi-stage: build the client bundle, embed it into the Go binary, ship a
# minimal static image. The final artifact is one process serving everything.

FROM node:24-alpine AS client
WORKDIR /src
COPY client/package.json client/package-lock.json client/
RUN cd client && npm ci
COPY client client
# Vite writes to ../internal/web/dist (see client/vite.config.ts).
RUN mkdir -p internal/web/dist && cd client && npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd cmd
COPY internal internal
COPY --from=client /src/internal/web/dist internal/web/dist
RUN CGO_ENABLED=0 go build -o /rogue ./cmd/rogue

FROM gcr.io/distroless/static-debian12
COPY --from=build /rogue /rogue
EXPOSE 8080
ENTRYPOINT ["/rogue"]
