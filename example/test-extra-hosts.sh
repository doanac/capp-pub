#!/bin/sh -e
[ "$(cat /etc/hosts | grep "foo")" == "1.2.3.4	foo" ] || (echo "=extra_hosts: FAIL"; exit 1)
[ "$(cat /etc/hosts | grep "bar")" == "4.3.2.1	bar" ] || (echo "=extra_hosts: FAIL"; exit 1)
echo "=extra_hosts: PASS"
