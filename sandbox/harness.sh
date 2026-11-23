#!/bin/bash
# usage: judge-harness <time_limit_seconds> <cmd...>
# Runs <cmd> once per /box/tests/N.in, writes /box/out/N.out, prints "N exit_code elapsed_s max_rss_kb".
# Runs as PID 1, which can't be killed from inside the container, so its result
# lines always get printed; the Go side rejects any extra ones user code adds.
set -u
tl=$1; shift
ulimit -s unlimited # deep recursion is normal in CP; the container memory limit still applies
ulimit -f 65536     # 64MB output cap per file, so a runaway print can't fill the host disk
for in in /box/tests/*.in; do
  n=$(basename "$in" .in)
  /usr/bin/time -q -f "%e %M" -o "/box/out/$n.meta" timeout -s KILL "$tl" "$@" < "$in" > "/box/out/$n.out" 2>/dev/null
  code=$?
  # `timeout` only kills its own child: a detached process (setsid) would keep
  # running into later tests, e.g. to write N.out after the time limit.
  # kill -1 signals everything we may signal except PID 1, i.e. except us.
  kill -9 -1 2>/dev/null
  find /tmp -mindepth 1 -delete 2>/dev/null # no state carried into the next test
  echo "$n $code $(tail -n1 "/box/out/$n.meta")"
done
