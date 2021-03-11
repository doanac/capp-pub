#!/bin/sh -e

touch /rw/test || (echo "rw-vol: FAIL"; exit 1)
echo "=rw-vol: PASS"

touch /tmpfs/test || (echo "tmpfs-vol: FAIL"; exit 1)
echo "=tmpfs-vol: PASS"

touch /bind-vol/test || (echo "bind-vol: FAIL"; exit 1)
echo "=bind-vol: PASS"
