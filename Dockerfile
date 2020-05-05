ARG version=1.14.60.5

FROM docker.io/library/golang:1.14-alpine AS builder
COPY . /mbs-manager
WORKDIR /mbs-manager
RUN CGO_ENABLED=0 go build ./cmd/mbs/

#FROM docker.io/library/golang:1.14 AS builder
#COPY . /mbs-manager
#WORKDIR /mbs-manager
#RUN CGO_ENABLED=1 go build -race ./cmd/mbs/

FROM localhost/bedrock-server:${version}
EXPOSE 8080
ENTRYPOINT [ "/usr/local/bin/mbs", "-listen", ":8080" ]
COPY --from=builder /mbs-manager/mbs /usr/local/bin/mbs
