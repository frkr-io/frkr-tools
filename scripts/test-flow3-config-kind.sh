#!/bin/bash
# Test Flow 3: Config-driven Kind K8s
# Outputs proof to: /tmp/flow3-proof.log

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PROOF_FILE="/tmp/flow3-proof.log"
rm -f "$PROOF_FILE"

cleanup() {
    pkill -f "frkr stream" 2>/dev/null || true
    pkill -f "node server.js" 2>/dev/null || true
    pkill -f "frkrup" 2>/dev/null || true
    pkill -f "kubectl port-forward" 2>/dev/null || true
    sleep 2
}

echo "=== Flow 3: Config-driven Kind K8s ===" | tee -a "$PROOF_FILE"
cleanup

echo "Checking/creating Kind cluster..." | tee -a "$PROOF_FILE"
if ! kind get clusters | grep -q "frkr-dev"; then
    echo "Creating Kind cluster..." | tee -a "$PROOF_FILE"
    kind create cluster --name frkr-dev
    sleep 15
fi

echo "Starting frkrup with Kind config..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR"
# frkrup will wait for Ctrl+C, so we run it in background and kill it after verification
rm -f /tmp/flow3-frkrup.log
./bin/frkrup --config examples/config-kind.yaml > /tmp/flow3-frkrup.log 2>&1 &
FRKRUP_PID=$!
# Wait for frkrup to complete setup or fail (max 4 minutes)
echo "Waiting for frkrup to complete setup..." | tee -a "$PROOF_FILE"
FRKRUP_SUCCESS=false
for i in {1..60}; do
    # Check if port forwarding is active (most reliable indicator frkrup succeeded)
    if lsof -i :8082 > /dev/null 2>&1 && lsof -i :8081 > /dev/null 2>&1; then
        # Verify gateways respond
        if curl -s --max-time 2 http://localhost:8082/health 2>&1 | grep -q "healthy" && \
           curl -s --max-time 2 http://localhost:8081/health 2>&1 | grep -q "healthy"; then
            echo "✅ frkrup setup complete (port forwarding active, gateways healthy)" | tee -a "$PROOF_FILE"
            FRKRUP_SUCCESS=true
            break
        fi
    fi
    # Check if frkrup succeeded - look for the success message
    if tail -50 /tmp/flow3-frkrup.log 2>/dev/null | grep -q "frkr is running on Kubernetes"; then
        echo "✅ frkrup setup complete" | tee -a "$PROOF_FILE"
        FRKRUP_SUCCESS=true
        sleep 3
        break
    fi
    # Check if frkrup failed
    if tail -50 /tmp/flow3-frkrup.log 2>/dev/null | grep -q "Kubernetes setup failed\|required deployment not ready"; then
        echo "❌ frkrup reported failure" | tee -a "$PROOF_FILE"
        tail -30 /tmp/flow3-frkrup.log >> "$PROOF_FILE" 2>&1
        cleanup
        exit 1
    fi
    sleep 4
done

if [ "$FRKRUP_SUCCESS" = "false" ]; then
    echo "❌ frkrup did not complete within timeout" | tee -a "$PROOF_FILE"
    tail -50 /tmp/flow3-frkrup.log >> "$PROOF_FILE" 2>&1
    cleanup
    exit 1
fi

# Port forwarding should already be active from the check above
# Verify gateways are accessible
echo "Verifying gateways are accessible..." | tee -a "$PROOF_FILE"
sleep 2
INGEST_HEALTH=$(curl -s --max-time 5 http://localhost:8082/health 2>&1)
STREAMING_HEALTH=$(curl -s --max-time 5 http://localhost:8081/health 2>&1)
    
if echo "$INGEST_HEALTH" | grep -q "healthy" && echo "$STREAMING_HEALTH" | grep -q "healthy"; then
    echo "✅ Gateways healthy" | tee -a "$PROOF_FILE"
    echo "Ingest health: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming health: $STREAMING_HEALTH" >> "$PROOF_FILE"
    
    echo "Starting example-api..." | tee -a "$PROOF_FILE"
    cd /home/jason/git/frkr-io/frkr-example-api
    pkill -f "node server.js" 2>/dev/null || true
    npm start > /tmp/flow3-api.log 2>&1 &
    sleep 4
    
    echo "Starting frkr-cli..." | tee -a "$PROOF_FILE"
    pkill -f "frkr stream" 2>/dev/null || true
    /home/jason/git/frkr-io/frkr-cli/bin/frkr stream my-api \
        --gateway-url=http://localhost:8081 \
        --username=testuser \
        --password=testpass \
        --forward-url=http://localhost:3001 \
        > /tmp/flow3-cli.log 2>&1 &
    sleep 4
    
    echo "Sending test requests..." | tee -a "$PROOF_FILE"
    curl -s http://localhost:3000/api/users > /dev/null
    curl -s -X POST http://localhost:3000/api/users \
        -H "Content-Type: application/json" \
        -d '{"name": "Flow3Test"}' > /dev/null
    sleep 4
    
    echo "Checking for mirrored traffic..." | tee -a "$PROOF_FILE"
    if grep -q "FORWARDED FROM FRKR" /tmp/flow3-api.log; then
        echo "✅ FLOW 3 PASSED - Mirrored traffic verified!" | tee -a "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "PROOF - Mirrored traffic entries:" >> "$PROOF_FILE"
        grep "FORWARDED FROM FRKR" /tmp/flow3-api.log >> "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "Full API logs:" >> "$PROOF_FILE"
        cat /tmp/flow3-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 0
    else
        echo "❌ FLOW 3 FAILED - No mirrored traffic" | tee -a "$PROOF_FILE"
        echo "API logs:" >> "$PROOF_FILE"
        cat /tmp/flow3-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 1
    fi
else
    echo "❌ FLOW 3 FAILED - Gateways not healthy via port-forward" | tee -a "$PROOF_FILE"
    echo "Ingest: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming: $STREAMING_HEALTH" >> "$PROOF_FILE"
    tail -30 /tmp/flow3-frkrup.log >> "$PROOF_FILE"
    cleanup
    pkill -P $FRKRUP_PID 2>/dev/null || true
    exit 1
fi
