#!/bin/sh
set -eu

awslocal sns create-topic --name murphy-events
