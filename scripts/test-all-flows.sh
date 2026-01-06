#!/bin/bash
# Master test script - runs all 4 flows and shows proof
# Each flow outputs proof to /tmp/flow{N}-proof.log

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== frkrup E2E Test Suite ===${NC}"
echo "Testing all 4 flows with verified mirrored traffic"
echo "Each flow outputs proof to /tmp/flow{N}-proof.log"
echo ""

results=()

# Flow 1: Config Docker Compose
echo -e "${GREEN}=== Flow 1: Config-driven Docker Compose ===${NC}"
if ./test-flow1-config-docker.sh 2>&1 | tee /tmp/flow1-output.log; then
    echo -e "${GREEN}✅ Flow 1 PASSED${NC}"
    results+=("Flow 1: ✅ PASSED")
else
    echo -e "${RED}❌ Flow 1 FAILED${NC}"
    results+=("Flow 1: ❌ FAILED")
fi
echo ""
sleep 5

# Flow 2: Interactive Docker Compose
echo -e "${GREEN}=== Flow 2: Interactive Docker Compose ===${NC}"
if ./test-flow2-interactive-docker.sh 2>&1 | tee /tmp/flow2-output.log; then
    echo -e "${GREEN}✅ Flow 2 PASSED${NC}"
    results+=("Flow 2: ✅ PASSED")
else
    echo -e "${RED}❌ Flow 2 FAILED${NC}"
    results+=("Flow 2: ❌ FAILED")
fi
echo ""
sleep 5

# Flow 3: Config Kind K8s
echo -e "${GREEN}=== Flow 3: Config-driven Kind K8s ===${NC}"
if ./test-flow3-config-kind.sh 2>&1 | tee /tmp/flow3-output.log; then
    echo -e "${GREEN}✅ Flow 3 PASSED${NC}"
    results+=("Flow 3: ✅ PASSED")
else
    echo -e "${RED}❌ Flow 3 FAILED${NC}"
    results+=("Flow 3: ❌ FAILED")
fi
echo ""
sleep 5

# Flow 4: Interactive Kind K8s
echo -e "${GREEN}=== Flow 4: Interactive Kind K8s ===${NC}"
if ./test-flow4-interactive-kind.sh 2>&1 | tee /tmp/flow4-output.log; then
    echo -e "${GREEN}✅ Flow 4 PASSED${NC}"
    results+=("Flow 4: ✅ PASSED")
else
    echo -e "${RED}❌ Flow 4 FAILED${NC}"
    results+=("Flow 4: ❌ FAILED")
fi

# Summary
echo ""
echo -e "${GREEN}=== Test Summary ===${NC}"
for result in "${results[@]}"; do
    echo "  $result"
done

echo ""
echo -e "${GREEN}=== Proof Files ===${NC}"
echo "  Flow 1: /tmp/flow1-proof.log"
echo "  Flow 2: /tmp/flow2-proof.log"
echo "  Flow 3: /tmp/flow3-proof.log"
echo "  Flow 4: /tmp/flow4-proof.log"
echo ""
echo "To view proof for any flow:"
echo "  cat /tmp/flow{N}-proof.log | grep -A 5 'PROOF'"

# Count passes
passed=$(printf '%s\n' "${results[@]}" | grep -c "✅" || true)
total=4

if [ "$passed" -eq 4 ]; then
    echo -e "${GREEN}✅ All flows passed!${NC}"
    exit 0
else
    echo -e "${RED}❌ Some flows failed (${passed}/4 passed)${NC}"
    exit 1
fi
