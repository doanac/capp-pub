#!/bin/sh -e

touch /must-fail || echo "=read_only: PASS"
[ "$(pwd)" == "/opt" ] || (echo "=cwd: FAIL"; exit 1)
echo "=cwd: PASS"
[ "$FOO" == "BAR" ] || (echo "=environment: FAIL"; exit 1)
echo "=environment: PASS"
[ "$(hostname)" == "foohost" ] || (echo "=hostname: FAIL"; exit 1)
echo "=hostname: PASS"
[ "$(sysctl kernel.domainname)" == "kernel.domainname = capp.io" ] || (echo "domainname: FAIL"; exit 1)
echo "=domainname: PASS"
