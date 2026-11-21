#!/bin/sh
set -eu

for database in catalog; do
  if ! psql -tAc "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -q 1; then
    psql -c "CREATE DATABASE $database"
  fi
done
