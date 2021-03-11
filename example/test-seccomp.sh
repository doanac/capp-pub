#!/bin/sh -e

mkdir -p /tmp/foo
chmod +x /tmp/foo && (echo "seccomp: FAIL"; exit 1)
echo "=seccomp: PASS"
