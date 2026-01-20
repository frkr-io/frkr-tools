#!/bin/bash
# Test Flow 2: Interactive Docker Compose
# Outputs proof to: /tmp/flow2-proof.log

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PROOF_FILE="/tmp/flow2-proof.log"
rm -f "$PROOF_FILE"

cleanup() {
    pkill -f "frkr stream" 2>/dev/null || true
    pkill -f "node server.js" 2>/dev/null || true
    pkill -f "frkrup" 2>/dev/null || true
    sleep 2
}

echo "=== Flow 2: Interactive Docker Compose ===" | tee -a "$PROOF_FILE"
cleanup

# Stop any existing Docker Compose to ensure clean state
echo "Ensuring clean state..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR/../frkr-infra-docker"
docker compose down 2>/dev/null || true
sleep 2

# frkrup will start Docker Compose automatically when prompted
echo "Starting frkrup interactively (frkrup will start Docker Compose)..." | tee -a "$PROOF_FILE"
cd "$SCRIPT_DIR"
expect << 'EOF' > /tmp/flow2-frkrup.log 2>&1 &
set timeout 120
spawn ../bin/frkrup
expect {
    "Deploy to Kubernetes?" { 
        send "no\r"
        exp_continue 
    }
    "Start Docker Compose?" {
        send "yes\r"
        exp_continue
    }
    "Use detected services?" { 
        send "yes\r"
        exp_continue 
    }
    "Use default configuration?" {
        send "yes\r"
        exp_continue
    }
    "Ingest gateway port" { 
        send "\r"
        exp_continue 
    }
    "Streaming gateway port" { 
        send "\r"
        exp_continue 
    }
    "Enable Test OIDC Provider?" {
        send "no\r"
        exp_continue
    }
    "Stream name" { 
        send "\r"
        exp_continue 
    }
    "gateway is healthy" {
        exp_continue
    }
    "frkr is running" {
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
# Wait for frkrup to complete setup
echo "Waiting for frkrup to complete setup..." | tee -a "$PROOF_FILE"
for i in {1..90}; do
    if tail -30 /tmp/flow2-frkrup.log 2>/dev/null | grep -q "frkr is running"; then
        echo "✅ frkrup setup complete" | tee -a "$PROOF_FILE"
        sleep 3
        break
    fi
    sleep 2
done

echo "Checking gateway health..." | tee -a "$PROOF_FILE"
INGEST_HEALTH=$(curl -s http://localhost:8082/health)
STREAMING_HEALTH=$(curl -s http://localhost:9081/health)

if echo "$INGEST_HEALTH" | grep -q "healthy" && (echo "$STREAMING_HEALTH" | grep -q "healthy" || echo "$STREAMING_HEALTH" | grep -q "OK"); then
    echo "✅ Gateways healthy" | tee -a "$PROOF_FILE"
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
    npm start > /tmp/flow2-api.log 2>&1 &
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
        > /tmp/flow2-cli.log 2>&1 &
    sleep 4
    
    echo "Sending test requests..." | tee -a "$PROOF_FILE"
    curl -s http://localhost:3000/api/users > /dev/null
    curl -s -X POST http://localhost:3000/api/users \
        -H "Content-Type: application/json" \
        -d '{"name": "Flow2Test"}' > /dev/null
    sleep 4
    
    echo "Checking for mirrored traffic..." | tee -a "$PROOF_FILE"
    if grep -q "FORWARDED FROM FRKR" /tmp/flow2-api.log; then
        echo "✅ FLOW 2 PASSED - Mirrored traffic verified!" | tee -a "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "PROOF - Mirrored traffic entries:" >> "$PROOF_FILE"
        grep "FORWARDED FROM FRKR" /tmp/flow2-api.log >> "$PROOF_FILE"
        echo "" >> "$PROOF_FILE"
        echo "Full API logs:" >> "$PROOF_FILE"
        cat /tmp/flow2-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 0
    else
        echo "❌ FLOW 2 FAILED - No mirrored traffic" | tee -a "$PROOF_FILE"
        echo "API logs:" >> "$PROOF_FILE"
        cat /tmp/flow2-api.log >> "$PROOF_FILE"
        cleanup
        pkill -P $FRKRUP_PID 2>/dev/null || true
        exit 1
    fi
else
    echo "❌ FLOW 2 FAILED - Gateways not healthy" | tee -a "$PROOF_FILE"
    echo "Ingest: $INGEST_HEALTH" >> "$PROOF_FILE"
    echo "Streaming: $STREAMING_HEALTH" >> "$PROOF_FILE"
    tail -30 /tmp/flow2-frkrup.log >> "$PROOF_FILE"
    cleanup
    pkill -P $FRKRUP_PID 2>/dev/null || true
    exit 1
fi
