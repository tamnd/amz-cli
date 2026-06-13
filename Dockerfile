# Consumed by GoReleaser: it copies the already cross-compiled binary out of the
# build context rather than compiling, so the image build is fast and uses the
# same static binary every other artifact ships.
#
# GoReleaser builds one multi-platform image with buildx and stages each
# platform's binary under a $TARGETPLATFORM directory (e.g. linux/amd64/) in the
# build context, so the COPY line selects the right one through the automatic
# TARGETPLATFORM build arg.
FROM alpine:3.21

ARG TARGETPLATFORM

# ca-certificates for HTTPS to amazon.com; tzdata for sane timestamps.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 amz \
 && mkdir -p /data \
 && chown amz:amz /data

COPY $TARGETPLATFORM/amz /usr/bin/amz

USER amz
WORKDIR /data

# All state lives under /data; mount a volume to keep the cache and local store:
#
#   docker run -v ~/data/amz:/data ghcr.io/tamnd/amz product B084DWG2VQ
ENV AMZ_DATA_DIR=/data
VOLUME ["/data"]

ENTRYPOINT ["/usr/bin/amz"]
