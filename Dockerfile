FROM debian:bookworm-slim

ARG TARGETPLATFORM

RUN useradd --uid 10001 --gid users --home-dir /nonexistent \
    --shell /usr/sbin/nologin --no-create-home logthing && \
    mkdir -p /data/messages && \
    chown -R logthing:users /data

COPY --chown=logthing:users bin/${TARGETPLATFORM}/logthing /usr/local/bin/
COPY --chown=logthing:users bin/${TARGETPLATFORM}/syslogsend /usr/local/bin/

ENV LOGTHING_HTTP_ADDR=:8080 \
    LOGTHING_SYSLOG_UDP_ADDR=:5514 \
    LOGTHING_SYSLOG_TCP_ADDR=:5514 \
    LOGTHING_SYSLOG_FORMAT=automatic \
    LOGTHING_DATA_DIR=/data/messages

VOLUME ["/data"]
EXPOSE 8080
EXPOSE 5514/tcp
EXPOSE 5514/udp

USER logthing
ENTRYPOINT ["/usr/local/bin/logthing"]