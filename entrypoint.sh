#!/bin/bash

umask ${UMASK}

if [ "$1" = "version" ]; then
  ./alist version
else
  chown -R ${PUID}:${PGID} /opt/alist/
  
  if [ "$RUN_ARIA2" = "true" ]; then
    exec su-exec ${PUID}:${PGID} nohup aria2c \
      --enable-rpc \
      --rpc-allow-origin-all \
      --conf-path=/root/.aria2/aria2.conf \
      >/dev/null 2>&1 &
  fi

  exec su-exec ${PUID}:${PGID} ./alist server --no-prefix
fi