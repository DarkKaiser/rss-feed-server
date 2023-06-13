#!/bin/bash
set -e

APP_BIN_FILE=/docker-entrypoint/dist/rss-feed-server
APP_CONFIG_FILE=/docker-entrypoint/dist/rss-feed-server.json

if [ -f "$APP_BIN_FILE" ]; then
  mv -f $APP_BIN_FILE /usr/local/app/
fi

if [ -f "$APP_CONFIG_FILE" ]; then
  mv -f $APP_CONFIG_FILE /usr/local/app/
fi

exec "$@"
