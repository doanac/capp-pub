#!/bin/sh -e

[ "$(sysctl net.core.somaxconn)" == "net.core.somaxconn = 1234" ] || (echo "=sysctls: FAIL"; exit 1)
echo "=sysctls: PASS"
