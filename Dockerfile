# The final image holds one static binary and no shell.
FROM node:25-alpine@sha256:bdf2cca6fe3dabd014ea60163eca3f0f7015fbd5c7ee1b0e9ccb4ced6eb02ef4 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /heirfold ./cmd/heirfold

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /heirfold /heirfold
ENV HEIRFOLD_DATA=/data
ENV HEIRFOLD_LISTEN=0.0.0.0:8080
VOLUME ["/data"]
EXPOSE 8080
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/heirfold", "healthcheck"]
ENTRYPOINT ["/heirfold"]
CMD ["serve"]
