#!/bin/bash
set -e

# Setup paths relative to script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TOOLS_DIR="$REPO_ROOT/frkr-tools"
CLI_DIR="$REPO_ROOT/frkr-cli"
INGEST_DIR="$REPO_ROOT/frkr-ingest-gateway"
STREAMING_DIR="$REPO_ROOT/frkr-streaming-gateway"
INFRA_DIR="$TOOLS_DIR/frkr-infra-docker"
SCRIPTS_DIR="$TOOLS_DIR/scripts"

# Create dummy server script
DUMMY_SERVER_SCRIPT="$SCRIPTS_DIR/dummy_server.py"

cat <<EOF > "$DUMMY_SERVER_SCRIPT"
#!/usr/bin/env python3
import http.server
import sys

class RequestHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        try:
            body = self.rfile.read(length).decode('utf-8')
        except:
            body = ""
        print(f"RECEIVED: {self.command} {self.path} {body}", flush=True)
        self.send_response(200)
        self.end_headers()
    
    def do_GET(self):
        print(f"RECEIVED: {self.command} {self.path}", flush=True)
        self.send_response(200)
        self.end_headers()

if __name__ == "__main__":
    port = 9999
    print(f"Starting dummy server on port {port}...", flush=True)
    http.server.HTTPServer(('', port), RequestHandler).serve_forever()
EOF

echo "🔨 Building Binaries..."
cd "$CLI_DIR" && go build -o "$TOOLS_DIR/bin/frkr" ./cmd/frkr
cd "$INGEST_DIR" && go build -o bin/gateway ./cmd/gateway
cd "$STREAMING_DIR" && go build -o bin/gateway ./cmd/gateway
cd "$TOOLS_DIR" && make build

# Explicitly start infrastructure
echo "🐳 Starting Docker Infrastructure..."
cd "$INFRA_DIR"
docker compose up -d
echo "⏳ Waiting for DB and Kafka..."
sleep 15

# Start Gateways Manually
echo "🚀 Starting Ingest Gateway..."
cd "$INGEST_DIR"
./bin/gateway \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --broker-url="localhost:19092" \
  --http-port=8082 > ingest.log 2>&1 &
INGEST_PID=$!
echo "Ingest PID: $INGEST_PID"

echo "🚀 Starting Streaming Gateway..."
cd "$STREAMING_DIR"
./bin/gateway \
  --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" \
  --broker-url="localhost:19092" \
  --http-port=8081 > streaming.log 2>&1 &
STREAMING_PID=$!
echo "Streaming PID: $STREAMING_PID"

echo "🚀 Starting Dummy Proxy Server..."
python3 "$DUMMY_SERVER_SCRIPT" > "$SCRIPTS_DIR/forwarded.log" 2>&1 &
DUMMY_PID=$!
echo "Dummy PID: $DUMMY_PID"

cleanup() {
    echo "🛑 Stopping Gateways and Dummy Server..."
    kill $INGEST_PID || true
    kill $STREAMING_PID || true
    kill $DUMMY_PID || true
    echo "🐳 Stopping Docker Infrastructure..."
    cd "$INFRA_DIR"
    docker compose down
    
    echo "🧹 Cleaning up temp files..."
    rm -f "$DUMMY_SERVER_SCRIPT"
    rm -f "$SCRIPTS_DIR/forwarded.log"
    
    echo "📜 Ingest Logs (Tail 20):"
    tail -n 20 "$INGEST_DIR/ingest.log" || true
    echo "📜 Streaming Logs (Tail 20):"
    tail -n 20 "$STREAMING_DIR/streaming.log" || true
}
trap cleanup EXIT

echo "⏳ Waiting for Gateways readiness..."
sleep 5
# Optionally Verify Health
if ! curl -s http://localhost:8082/health/ready | grep -q 'healthy'; then
    echo "⚠️ Ingest Gateway not ready yet..."
    sleep 5
fi

echo "🔧 Configuring Stream and User..."
"$TOOLS_DIR/bin/frkrcfg" stream create test-replay --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" || true
"$TOOLS_DIR/bin/frkrcfg" user create replay-user --db-url="postgres://root@localhost:26257/frkrdb?sslmode=disable" --password="replay-pass" || true

echo "📡 Sending Traffic..."
# T1
T1=$(date -u +%Y-%m-%dT%H:%M:%SZ)
echo "T1: $T1"
sleep 1
curl -s -X POST http://localhost:8082/ingest -u replay-user:replay-pass -d '{"stream_id": "test-replay", "request": {"request_id": "req-1", "method": "POST", "path": "/one"}}'
sleep 2

# T2
T2=$(date -u +%Y-%m-%dT%H:%M:%SZ)
echo "T2: $T2"
sleep 1
curl -s -X POST http://localhost:8082/ingest -u replay-user:replay-pass -d '{"stream_id": "test-replay", "request": {"request_id": "req-2", "method": "POST", "path": "/two"}}'
sleep 2

# T3
T3=$(date -u +%Y-%m-%dT%H:%M:%SZ)
echo "T3: $T3"
sleep 1
curl -s -X POST http://localhost:8082/ingest -u replay-user:replay-pass -d '{"stream_id": "test-replay", "request": {"request_id": "req-3", "method": "POST", "path": "/three"}}'
sleep 2

echo "🧪 Verifying Replay..."

FRKR_BIN="$TOOLS_DIR/bin/frkr"
FORWARDED_LOG="$SCRIPTS_DIR/forwarded.log"

# Truncate log first
> "$FORWARDED_LOG"

# 1. Replay from T2 (Should see req-2, req-3)
echo "👉 Scenario 1: Replay --from $T2"
"$FRKR_BIN" stream test-replay \
  --gateway localhost:8081 \
  --username replay-user \
  --password replay-pass \
  --from "$T2" \
  --forward-url http://localhost:9999 \
  --forward-timeout 1 \
  --insecure > "$SCRIPTS_DIR/output_replay_1.log" 2>&1 &
REPLAY_PID=$!
sleep 5
kill $REPLAY_PID || true

echo "📄 Scenario 1 Captured Logs:"
cat "$FORWARDED_LOG"

if grep -q "POST /two" "$FORWARDED_LOG" && grep -q "POST /three" "$FORWARDED_LOG"; then
    echo "✅ Scenario 1 Passed: Received req-2 and req-3"
else
    echo "❌ Scenario 1 Failed"
    exit 1
fi

if grep -q "POST /one" "$FORWARDED_LOG"; then
     echo "❌ Scenario 1 Failed: Received req-1 but shouldn't have"
     exit 1
fi

# Reset Log
> "$FORWARDED_LOG"

# 2. Fixed Window T2 to T3 (Should see req-2 ONLY)
echo "👉 Scenario 2: Replay --from $T2 --to $T3"
"$FRKR_BIN" stream test-replay \
  --gateway localhost:8081 \
  --username replay-user \
  --password replay-pass \
  --from "$T2" \
  --to "$T3" \
  --forward-url http://localhost:9999 \
  --forward-timeout 1 \
  --insecure > "$SCRIPTS_DIR/output_replay_2.log" 2>&1 &
REPLAY_PID_2=$!
sleep 5
kill $REPLAY_PID_2 || true

echo "📄 Scenario 2 Captured Logs:"
cat "$FORWARDED_LOG"

if grep -q "POST /two" "$FORWARDED_LOG"; then
    echo "✅ Scenario 2 Passed: Received req-2"
else
    echo "❌ Scenario 2 Failed: Did not receive req-2"
    exit 1
fi

if grep -q "POST /three" "$FORWARDED_LOG"; then
    echo "❌ Scenario 2 Failed: Received req-3 (should be excluded)"
    exit 1
fi

echo "🎉 All Scenarios Passed!"
