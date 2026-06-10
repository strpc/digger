ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-alpine as builder

RUN apk add --no-cache --update git

WORKDIR /build
COPY ./go.mod ./
RUN go mod download -x

COPY ./ .

ARG PROJECT_PATH
ARG VERSION
ARG COMMIT_HASH

ARG GOARCH
ARG GOOS
ARG TARGETVARIANT
ARG CGO_ENABLED=0

RUN \
  CGO_ENABLED=$CGO_ENABLED \
  GOARCH=$GOARCH \
  GOOS=$GOOS \
  GOARM=$([ "$TARGETVARIANT" = "v7" ] && echo "7" || echo "") \
  go build \
  -ldflags=" \
  -X 'main.version=$VERSION' \
  -X 'main.commitHash=$COMMIT_HASH' \
  " \
  -o ./app ./$PROJECT_PATH


FROM alpine:3.24

RUN adduser -u 1000 -h /app -D -g "" user  \
  && chown -hR user: /app

WORKDIR /app

COPY --from=builder --chown=user:user /build/app .

USER user

CMD ["./app"]
