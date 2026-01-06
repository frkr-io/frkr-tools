# Quick Start Guide

Get frkr up and running in 3 steps.

## Prerequisites

- Docker & Docker Compose
- Go 1.21+
- Node.js 18+

---

## Step 1: Clone Repositories

```bash
# Clone frkr-tools with submodules (includes infrastructure)
git clone --recurse-submodules https://github.com/frkr-io/frkr-tools.git

# Clone the example API
git clone https://github.com/frkr-io/frkr-example-api.git
```

---

## Step 2: Start frkr

```bash
cd frkr-tools

# Build and run frkrup
make build
./bin/frkrup
```

**What happens:**
1. `frkrup` auto-detects if services are running and asks minimal questions
2. If services aren't running, `frkrup` will offer to start Docker Compose automatically
3. Press Enter to accept all defaults (most prompts have smart defaults)
4. `frkrup` will:
   - Auto-detect running services on default ports
   - Start CockroachDB and Redpanda (if needed)
   - Run database migrations automatically
   - Create the default stream (`my-api`)
   - Start both gateways
   - Stream gateway logs with health check status

**Default configuration (auto-detected):**
- Database: `localhost:26257` (PostgreSQL-compatible, works with CockroachDB or Postgres)
- Broker: `localhost:19092` (Kafka-compatible, works with Redpanda or Kafka)
- Ingest Gateway: `http://localhost:8082`
- Streaming Gateway: `http://localhost:8081`
- Stream: `my-api`

**Simplified prompts:**
- If services are detected, you'll only be asked to confirm using them
- If services aren't detected, you can use defaults or customize
- Gateway ports and stream name can be customized if needed

**Keep this terminal open** - `frkrup` stays running and streams gateway logs. Press `Ctrl+C` to stop everything.

---

## Step 3: Start Example API

**In a new terminal:**

```bash
cd frkr-example-api

# Install dependencies
npm install

# Start the API (uses defaults: http://localhost:8082, stream: my-api)
npm start
```

The API runs on:
- **Port 3000**: Direct API calls (mirrored to frkr)
- **Port 3001**: Forwarded requests from frkr CLI

---

## Test It

**In another terminal:**

```bash
# Send a test request (will be mirrored to frkr)
curl http://localhost:3000/api/users

# Send a POST request
curl -X POST http://localhost:3000/api/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice"}'
```

Watch the `frkrup` terminal - you should see the requests being ingested!

---

## Stream with CLI

To forward mirrored requests back to your API:

```bash
git clone https://github.com/frkr-io/frkr-cli.git
cd frkr-cli
make build

./bin/frkr stream my-api \
  --gateway-url=http://localhost:8081 \
  --username=testuser \
  --password=testpass \
  --forward-url=http://localhost:3001
```

Watch the `frkr-example-api` terminal - you'll see mirrored requests labeled as **FORWARDED FROM FRKR**.

---

## What's Next?

- **Route-based routing**: See [Node SDK README](https://github.com/frkr-io/frkr-sdk-node/README.md#route-based-stream-routing) for sending different routes to different streams
- **Kubernetes setup**: See [Kubernetes Quick Start Guide](K8S-QUICKSTART.md) for Kubernetes deployment
- **Full documentation**: See [README](README.md) for advanced usage

---

## Troubleshooting

**Auto-detection not working?**
- `frkrup` checks ports `26257` (database) and `19092` (broker) on `localhost`
- If services are running on different ports, choose "no" when asked to use detected services
- You'll then be prompted for custom configuration

**Ports already in use?**
- Stop any existing services: `docker compose down` (in `frkr-infra-docker`)
- Or change ports in `frkrup` prompts (when customizing configuration)

**Can't connect to database/broker?**
- Ensure Docker Compose services are running: `docker ps | grep -E "(cockroach|redpanda)"`
- If `frkrup` didn't start them automatically, start manually: `cd frkr-tools/frkr-infra-docker && docker compose up -d`
- Wait a few seconds for services to be ready, then try again

**Database/Broker compatibility:**
- Works with **PostgreSQL** or **CockroachDB** (auto-detected)
- Works with **Kafka** or **Redpanda** (both use standard Kafka protocol)
- No vendor-specific configuration needed

**Need to reset everything?**
- Stop `frkrup` (Ctrl+C)
- Stop Docker Compose: `cd frkr-tools/frkr-infra-docker && docker compose down -v`
- Restart from Step 2

