#!/bin/sh -e

[ "$(cat /proc/self/oom_score_adj)" == "1" ] || (echo "=oom_score_adj: FAIL"; exit 1)
echo "=oom_score_adj: PASS"
