#!/bin/bash

chown -R ${PUID}:${PGID} /opt/alist/

umask ${UMASK}

if [ "$RUN_ARIA2" = "true" ]; then
  exec su-exec ${PUID}:${PGID} nohup aria2c \
    --enable-rpc \
    --rpc-allow-origin-all \
    --conf-path=/root/.aria2/aria2.conf \
    >/dev/null 2>&1 &
fi

if [ "$1" = "version" ]; then
  ./alist version
else
  exec su-exec ${PUID}:${PGID} ./alist server --no-prefix
fi