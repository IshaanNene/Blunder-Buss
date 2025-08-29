FROM ubuntu:24.04 AS builder
ARG MAKE_JOBS=1

RUN apt-get update && apt-get install -y --no-install-recommends git build-essential ca-certificates curl && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN git clone https://github.com/official-stockfish/Stockfish.git /usr/src/stockfish

WORKDIR /usr/src/stockfish/src

RUN make build -j${MAKE_JOBS}
RUN mkdir -p /tmp/nnue && cp -n /usr/src/stockfish/src/*.nnue /tmp/nnue/ || true

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends libgomp1 socat netcat-openbsd ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/src/stockfish/src/stockfish /usr/local/bin/stockfish
COPY --from=builder /tmp/nnue /usr/local/share/stockfish/nnue

RUN chmod +x /usr/local/bin/stockfish
RUN useradd -r -s /bin/false stockfish

EXPOSE 4000
CMD ["socat", "TCP-LISTEN:4000,fork,reuseaddr", "EXEC:/usr/local/bin/stockfish"]