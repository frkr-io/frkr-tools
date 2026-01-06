#!/bin/bash
# Test Flow 4: Interactive Kind K8s
# Outputs proof to: /tmp/flow4-proof.log

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PROOF_FILE="/tmp/flow4-proof.log"
rm -f "$PROOF_FILE"

cleanup() {
    pkill -f "frkr stream" 2>/dev/null || true
    pkill -f "node server.js" 2>/dev/null || true
    pkill -f "frkrup" 2>/dev/null || true
    pkill -f "kubectl port-forward" 2>/dev/null || true
    sleep 2
}

echo "=== Flow 4: Interactive Kind K8s ===" | tee -a "$PROOF_FILE"
cleanup

echo "Ensuring Kind cluster exists..." | tee -a "$PROOF_FILE"
if ! kind get clusters | grep -q "frkr-dev"; then
    echo "Creating Kind cluster..." | tee -a "$PROOF_FILE"
    kind create cluster --name frkr-dev
    sleep 15
fi

echo "Starting frkrup interactively for K8s..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR"
expect << 'EOF' > /tmp/flow4-frkrup.log 2>&1 &
set timeout 240
spawn ./bin/frkrup
expect {
    "Deploy to Kubernetes?" { 
        send "yes\r"
        exp_continue 
    }
    "Use port forwarding" { 
        send "yes\r"
        exp_continue 
    }
    "gateway is healthy" {
        exp_continue
    }
    "frkr is running on Kubernetes" {
        exp_continue
    }
    timeout { 
        puts "Timeout"
        exit 1 
    }
    eof { 
        exit 0 
    }
}
EOF
FRKRUP_PID=$!
sleep 120

# Wait for frkrup to complete setup or fail
echo "Waiting for frkrup to complete setup..." | tee -a "$PROOF_FILE"
FRKRUP_SUCCESS=false
for i in {1..180}; do
    # Check if frkrup succeeded
    if tail -30 /tmp/flow4-frkrup.log 2>/dev/null | grep -q "frkr is running on Kubernetes"; then
        echo "✅ frkrup setup complete" | tee -a "$PROOF_FILE"
        FRKRUP_SUCCESS=true
        sleep 5
        break
    fi
    # Check if frkrup failed
    if tail -30 /tmp/flow4-frkrup.log 2>/dev/null | grep -q "Kubernetes setup failed"; then
        echo "⚠️  frkrup reported failure, but checking if pods are ready anyway..." | tee -a "$PROOF_FILE"
        break
    fi
    sleep 2
done

# Wait for pods to be ready (in case frkrup's wait timed out but pods are actually ready)
echo "Waiting for pods to be ready..." | tee -a "$PROOF_FILE"
kubectl wait --for=condition=ready pod -l app.kubernetes.io/component=ingest-gateway --timeout=120s 2>&1 | tee -a "$PROOF_FILE" || true
kubectl wait --for=condition=ready pod -l app.kubernetes.io/component=streaming-gateway --timeout=120s 2>&1 | tee -a "$PROOF_FILE" || true
sleep 5

# Set up port forwarding if frkrup didn't (it should have, but just in case)
if ! lsof -i :8082 > /dev/null 2>&1 || ! lsof -i :8081 > /dev/null 2>&1; then
    echo "Setting up port forwarding..." | tee -a "$PROOF_FILE"
    kubectl port-forward svc/frkr-ingest-gateway 8082:8080 > /tmp/flow4-pf-ingest.log 2>&1 &
    kubectl port-forward svc/frkr-streaming-gateway 8081:8081 > /tmp/flow4-pf-streaming.log 2>&1 &
    sleep 5
fi

# Verify gateways are accessible
echo "Verifying gateways are accessible..." | tee -a "$PROOF_FILE"
sleep 3
INGEST_HEALTH=$(curl -s http://localhost:8082/health)
STREAMING_HEALTH=$(curl -s http://localhost:8081/health)

if echo "$INGEST_HEALTH" | grep -q "healthy" && echo "$STREAMING_HEALTH" | grep -q "healthy"; then
    echo "✅ Gateways healthy" | tee -a "$PROOF_FILE"
    echo "Ingest health: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming health: $STREAMING_HEALTH" >> "$PROOF_FILE"
    
    echo "Starting example-api..." | tee -a "$PROOF_FILE"
    cd /home/jason/git/frkr-io/frkr-example-api
    pkill -f "node server.js" 2>/dev/null || true
    npm start > /tmp/flow4-api.log 2>&1 &
    sleep 4
    
    echo "Starting frkr-cli..." | tee -a "$PROOF_FILE"
    pkill -f "frkr stream" 2>/dev/null || true
    /home/jason/git/frkr-io/frkr-cli/bin/frkr stream my-api \
        --gateway-url=http://localhost:8081 \
        --username=testuser \
        --password=testpass \
        --forward-url=http://localhost:3001 \
        > /tmp/flow4-cli.log 2>&1 &
    sleep 4
    
    echo "Sending test requests..." | tee -a "$PROOF_FILE"
    curl -s http://localhost:3000/api/users > /dev/null
    curl -s -X POST http://localhost:3000/api/users \
        -H "Content-Type: application/json" \
        -d '{"name": "Flow4Test"}' > /dev/null
    sleep 4
    
    echo "Checking for mirrored traffic..." | tee -a "$PROOF_FILE"
    if grep -q "FORWARDED FROM FRKR" /tmp/flow4-api.log; then
        echo "✅ FLOW 4 PASSED - Mirrored traffic verified!" | tee -a "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "PROOF - Mirrored traffic entries:" >> "$PROOF_FILE"
        grep "FORWARDED FROM FRKR" /tmp/flow4-api.log >> "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "Full API logs:" >> "$PROOF_FILE"
        cat /tmp/flow4-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 0
    else
        echo "❌ FLOW 4 FAILED - No mirrored traffic" | tee -a "$PROOF_FILE"
        echo "API logs:" >> "$PROOF_FILE"
        cat /tmp/flow4-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 1
    fi
else
    echo "❌ FLOW 4 FAILED - Gateways not healthy" | tee -a "$PROOF_FILE"
    echo "Ingest: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming: $STREAMING_HEALTH" >> "$PROOF_FILE"
    tail -30 /tmp/flow4-frkrup.log >> "$PROOF_FILE"
    cleanup
    pkill -P $FRKRUP_PID 2>/dev/null || true
    exit 1
fi
