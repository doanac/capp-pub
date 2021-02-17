#!/bin/sh -e

[ "$(id -u)" == "10" ] || (echo "user: FAIL"; exit 1)
echo "=user: PASS"
[ "$(id -g)" == "20" ] || (echo "group: FAIL"; exit 1)
echo "=group: PASS"
