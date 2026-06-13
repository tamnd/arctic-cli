# Consumed by GoReleaser: it copies the already cross-compiled binary out of the
# build context rather than compiling, so the image build is fast and uses the
# same static binary every other artifact ships.
FROM alpine:3.21

ARG TARGETPLATFORM

# ca-certificates for HTTPS to the API and Hugging Face; tzdata for sane timestamps.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 arctic \
 && mkdir -p /data \
 && chown arctic:arctic /data

COPY $TARGETPLATFORM/arctic /usr/bin/arctic

USER arctic
WORKDIR /data

# All state lives under /data; mount a volume to keep imports and the index:
#
#   docker run -v ~/data/arctic:/data ghcr.io/tamnd/arctic sub golang
ENV ARCTIC_DATA_DIR=/data
VOLUME ["/data"]

ENTRYPOINT ["/usr/bin/arctic"]
