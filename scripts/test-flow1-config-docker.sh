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
cd "$SCRIPT_DIR/frkr-infra-docker"
docker compose down 2>/dev/null || true
sleep 2

# frkrup will start Docker Compose automatically, but it prompts even in config mode
# So we pipe "yes" to it when it asks to start Docker Compose
echo "Starting frkrup with config (frkrup will start Docker Compose)..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR"
echo "yes" | timeout 90 ./bin/frkrup --config examples/config-docker-compose.yaml > /tmp/flow1-frkrup.log 2>&1 &
FRKRUP_PID=$!
# Wait for frkrup to start Docker Compose and gateways
sleep 50

echo "Checking gateway health..." | tee -a "$PROOF_FILE"
INGEST_HEALTH=$(curl -s http://localhost:8082/health)
STREAMING_HEALTH=$(curl -s http://localhost:8081/health)

if echo "$INGEST_HEALTH" | grep -q "healthy" && echo "$STREAMING_HEALTH" | grep -q "healthy"; then
    echo "✅ Gateways healthy" | tee -a "$PROOF_FILE"
    echo "Ingest health: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming health: $STREAMING_HEALTH" >> "$PROOF_FILE"
    
    echo "Starting example-api..." | tee -a "$PROOF_FILE"
    cd /home/jason/git/frkr-io/frkr-example-api
    pkill -f "node server.js" 2>/dev/null || true
    npm start > /tmp/flow1-api.log 2>&1 &
    sleep 4
    
    echo "Starting frkr-cli..." | tee -a "$PROOF_FILE"
    pkill -f "frkr stream" 2>/dev/null || true
    /home/jason/git/frkr-io/frkr-cli/bin/frkr stream my-api \
        --gateway-url=http://localhost:8081 \
        --username=testuser \
        --password=testpass \
        --forward-url=http://localhost:3001 \
        > /tmp/flow1-cli.log 2>&1 &
    sleep 4
    
    echo "Sending test requests..." | tee -a "$PROOF_FILE"
    curl -s http://localhost:3000/api/users > /dev/null
    curl -s -X POST http://localhost:3000/api/users \
        -H "Content-Type: application/json" \
        -d '{"name": "Flow1Test"}' > /dev/null
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
