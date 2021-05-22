#!/bin/sh -e
[ "$(cat /etc/resolv.conf | grep "nameserver")" == "nameserver 8.8.8.8" ] || (echo "=dns: FAIL"; exit 1)
[ "$(cat /etc/resolv.conf | grep "options")" == "options use-vc " ] || (echo "=dns_opt: FAIL"; exit 1)
[ "$(cat /etc/resolv.conf | grep "search")" == "search google.com " ] || (echo "=dns_search: FAIL"; exit 1)
nslookup google.com || (echo "=dns: FAIL"; exit 1)
echo "=dns: PASS"
