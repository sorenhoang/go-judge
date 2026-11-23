FROM gcc:14-bookworm
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 time \
 && rm -rf /var/lib/apt/lists/*
COPY harness.sh /usr/local/bin/judge-harness
RUN chmod 0755 /usr/local/bin/judge-harness
