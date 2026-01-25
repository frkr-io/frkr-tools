#!/bin/bash
# Test Flow 1: Config-driven Docker Compose
# Outputs proof to: /tmp/flow1-proof.log

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PROOF_FILE="/tmp/flow1-proof.log"
rm -f "$PROOF_FILE"

cleanup() {
    pkill -f "frkr stream" 2>/dev/null || true
    pkill -f "node server.js" 2>/dev/null || true
    pkill -f "frkrup" 2>/dev/null || true
    sleep 2
}

echo "=== Flow 1: Config-driven Docker Compose ===" | tee -a "$PROOF_FILE"
cleanup

# Stop any existing Docker Compose to ensure clean state
echo "Ensuring clean state..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR/../frkr-infra-docker"
docker compose down -v --remove-orphans 2>/dev/null || true
docker rm -f frkr-redpanda frkr-postgres 2>/dev/null || true
docker network prune -f 2>/dev/null || true
rm -f /tmp/flow1-*.log # clean logs too
sleep 10

# frkrup will start Docker Compose automatically, but it prompts even in config mode
# So we pipe "yes" to it when it asks to start Docker Compose
echo "Starting frkrup with config (frkrup will start Docker Compose)..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR"
echo "yes" | timeout 90 ../bin/frkrup --config ../examples/config-docker-compose.yaml > /tmp/flow1-frkrup.log 2>&1 &
FRKRUP_PID=$!
# Wait for frkrup to start Docker Compose and gateways
# Wait for frkrup to start Docker Compose and gateways
# Sleep a bit for startup, then poll
sleep 10
echo "Waiting for gateways to be healthy..." | tee -a "$PROOF_FILE"
for i in {1..60}; do
    INGEST_HEALTH=$(curl -s http://localhost:8082/health || echo "connection failed")
    STREAMING_HEALTH=$(curl -s http://localhost:9081/health || echo "connection failed")
    
    if echo "$INGEST_HEALTH" | grep -q "healthy" && (echo "$STREAMING_HEALTH" | grep -q "healthy" || echo "$STREAMING_HEALTH" | grep -q "OK"); then
        echo "✅ Gateways healthy" | tee -a "$PROOF_FILE"
        echo "Ingest health: $INGEST_HEALTH" >> "$PROOF_FILE"
        echo "Streaming health: $STREAMING_HEALTH" >> "$PROOF_FILE"
        break
    fi 
    sleep 2
done

# We check again if healthy, but use the last captured values
if echo "$INGEST_HEALTH" | grep -q "healthy" && (echo "$STREAMING_HEALTH" | grep -q "healthy" || echo "$STREAMING_HEALTH" | grep -q "OK"); then
    # Already printed valid status above
    :

    echo "Ingest health: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming health: $STREAMING_HEALTH" >> "$PROOF_FILE"
    
    echo "Creating stream and user..." | tee -a "$PROOF_FILE"
    DB_URL="postgres://root:password@localhost:5432/frkr?sslmode=disable"
    $SCRIPT_DIR/../bin/frkrcfg stream create my-api --db-url="$DB_URL" >> "$PROOF_FILE" 2>&1 || true
    $SCRIPT_DIR/../bin/frkrcfg user create testuser --db-url="$DB_URL" --password="testpass" >> "$PROOF_FILE" 2>&1 || true
    
    echo "Starting example-api..." | tee -a "$PROOF_FILE"
    # Assuming sibling directory structure
    cd "$SCRIPT_DIR/../../frkr-example-api" || cd "$HOME/git/frkr-io/frkr-example-api"
    pkill -f "node server.js" 2>/dev/null || true
    FRKR_PASSWORD="testpass" FRKR_USERNAME="testuser" npm start > /tmp/flow1-api.log 2>&1 &
    sleep 4
    
    echo "Starting frkr-cli..." | tee -a "$PROOF_FILE"
    pkill -f "frkr stream" 2>/dev/null || true
    
    # Use relative path or standard install location
    CLI_BIN="$SCRIPT_DIR/../../frkr-cli/bin/frkr"
    if [ ! -f "$CLI_BIN" ]; then
        CLI_BIN="frkr" # Fallback to PATH
    fi
    
    $CLI_BIN stream my-api \
        --gateway=localhost:8081 \
        --username=testuser \
        --password=testpass \
        --insecure \
        --forward-url=http://localhost:3001 \
        > /tmp/flow1-cli.log 2>&1 &
    sleep 4
    
    echo "Sending test requests..." | tee -a "$PROOF_FILE"
    # Wrap in || true to allow script to verify logs even if curl reports connection error (though log verification will likely fail)
    curl -s http://localhost:3000/api/users > /dev/null || echo "⚠️  Failed to call API (GET)" | tee -a "$PROOF_FILE"
    curl -s -X POST http://localhost:3000/api/users \
        -H "Content-Type: application/json" \
        -d '{"name": "Flow1Test"}' > /dev/null || echo "⚠️  Failed to call API (POST)" | tee -a "$PROOF_FILE"
    sleep 4
    
    echo "Checking for mirrored traffic..." | tee -a "$PROOF_FILE"
    if grep -q "FORWARDED FROM FRKR" /tmp/flow1-api.log; then
        echo "✅ FLOW 1 PASSED - Mirrored traffic verified!" | tee -a "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "PROOF - Mirrored traffic entries:" >> "$PROOF_FILE"
        grep "FORWARDED FROM FRKR" /tmp/flow1-api.log >> "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "Full API logs:" >> "$PROOF_FILE"
        cat /tmp/flow1-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 0
    else
        echo "❌ FLOW 1 FAILED - No mirrored traffic" | tee -a "$PROOF_FILE"
        echo "API logs:" >> "$PROOF_FILE"
        cat /tmp/flow1-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 1
    fi
else
    echo "❌ FLOW 1 FAILED - Gateways not healthy" | tee -a "$PROOF_FILE"
    echo "Ingest: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming: $STREAMING_HEALTH" >> "$PROOF_FILE"
    tail -30 /tmp/flow1-frkrup.log >> "$PROOF_FILE"
    cleanup
    pkill -P $FRKRUP_PID 2>/dev/null || true
    exit 1
fi
