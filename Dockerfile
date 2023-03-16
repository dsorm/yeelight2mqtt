# use ubuntu focal as base image
# builder stage
FROM golang:1.20 AS builder
USER root

# copy source files
WORKDIR /usr/src/app
COPY . .

# get dependencies and compile
RUN /usr/local/go/bin/go build -v -ldflags "-X main.Version=$(git describe --tags) -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S') -X main.GitCommit=$(git rev-parse HEAD)" github.com/dsorm/yeelight2mqtt

# final image stage
FROM ubuntu:jammy

# copy artifacts and needed files
RUN mkdir /app
COPY --from=builder /usr/src/app/yeelight2mqtt /app/yeelight2mqtt

# run
WORKDIR /app
CMD ["./yeelight2mqtt"]