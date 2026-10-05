ARG CERTS_IMAGE
ARG CERTS_PLATFORM=linux/amd64
FROM --platform=${CERTS_PLATFORM} ${CERTS_IMAGE} AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
ARG LINGON_BIN=bin/lingon
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY ${LINGON_BIN} /usr/bin/lingon
WORKDIR /lingon
ENV LOG_MODE=json
ENV HOME=/lingon
EXPOSE 12843
ENTRYPOINT ["/usr/bin/lingon"]
