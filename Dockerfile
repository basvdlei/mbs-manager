FROM debian:bullseye-slim AS unpacker
ARG version=1.16.0.2
ARG license=notaccepted
RUN if [ "${license}" != "accept" ]; then \
    echo "License not accepted. Please go to" \
         "https://www.minecraft.net/en-us/download/server/bedrock/" \
         "read the documents (like EULA and Privacy policy) that are" \
         "required to download the Minecraft Bedrock Server." \
         "After accepting rerun the build with '--build-arg license=accept'." \
         >&2 ; exit 126 ; fi
RUN apt-get update && apt-get install -y --no-install-recommends unzip
ADD https://minecraft.azureedge.net/bin-linux/bedrock-server-${version}.zip \
    /bedrock-server/server.zip
WORKDIR /bedrock-server
RUN unzip server.zip && rm server.zip && chmod 0755 bedrock_server

FROM debian:bullseye-slim AS bedrock-server
RUN apt-get update && \
    apt-get install -y libcurl4 libssl1.1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
ENV LD_LIBRARY_PATH=/usr/local/bedrock-server
COPY --from=unpacker /bedrock-server /usr/local/bedrock-server
WORKDIR /usr/local/bedrock-server
ENTRYPOINT [ "/usr/local/bedrock-server/bedrock_server" ]

FROM docker.io/library/golang:1.20-alpine AS builder
COPY . /mbs-manager
WORKDIR /mbs-manager
RUN CGO_ENABLED=0 go build ./cmd/mbs/

FROM bedrock-server
EXPOSE 8080
ENTRYPOINT [ "/usr/local/bin/mbs", "-listen", ":8080" ]
COPY --from=builder /mbs-manager/mbs /usr/local/bin/mbs
