#!/bin/sh
set -eu

nats stream info events >/dev/null 2>&1 || nats stream add events \
  --subjects 'events.>' \
  --retention interest \
  --storage file \
  --max-age 7d \
  --dupe-window 2m \
  --replicas 1 \
  --defaults
