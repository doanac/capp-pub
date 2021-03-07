#!/bin/sh -e

echo "TEST1" > /var/file || true
[ ! -f "/var/file" ] || (echo "deny read-only write: FAIL"; exit 1)
echo "=read-only: PASS"
echo "TEST2" > /var/test/file || true
[ -f "/var/test/file" ] || (echo "tmpfs write: FAIL"; exit 0)
echo "=tmpfs-write: PASS"
