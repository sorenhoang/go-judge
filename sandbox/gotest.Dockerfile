# Sandbox for `go test` problems. Pre-compiles the stdlib + testing deps into
# GOCACHE so a submission only compiles its own two files (~1s instead of a
# ~30s cold build). The runner mounts an anonymous volume over /gocache, which
# Docker seeds from this layer: writable per run, discarded with --rm, never
# shared between submissions.
FROM golang:1.25

ENV GOCACHE=/gocache GOTOOLCHAIN=local GOFLAGS=-buildvcs=false

RUN mkdir /warm && cd /warm \
 && printf 'module warm\n\ngo 1.21\n' > go.mod \
 && printf 'package warm\n\nimport "testing"\n\nfunc TestWarm(t *testing.T) {}\n' > warm_test.go \
 && go test -json ./... \
 && cd / && rm -rf /warm \
 && chown -R 65534:65534 /gocache
