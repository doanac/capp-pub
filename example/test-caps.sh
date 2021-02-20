#!/bin/sh -e
[ "$(cat /proc/self/status | grep CapEff)" == "CapEff:	000001ffffffffff" ] || (echo "=privileged: FAIL"; exit 1)
echo "=privileged: PASS"
