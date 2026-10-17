#!/usr/bin/env bash
# Integrationstest des nativen Clients gegen den Server-Dev-Stack.
# Das Serververzeichnis wird nur gelesen/gestartet, nie verändert.
#
# Der Dev-Stack published das ssh-gateway offiziell auf 127.0.0.1:2222
# (compose.dev.yml, siehe EXCHANGE.md). Node/Key via uucpadm anlegen;
# der Sync-Job rendert authorized_keys und Taylor-sys (Dev: ~3 s).
set -euo pipefail

CLIENT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
SERVER_DIR="$(cd "$CLIENT_DIR/../server" && pwd)"
COMPOSE=(docker compose -f "$SERVER_DIR/compose/compose.yml" -f "$SERVER_DIR/compose/compose.dev.yml")

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export HOME="$WORK/home"   # Client-Config isolieren (os.UserConfigDir)
mkdir -p "$HOME"
ADDR="itest@uucpmail.de"

echo "== Dev-Stack starten =="
(cd "$SERVER_DIR" && "${COMPOSE[@]}" up -d --build)

echo "== Client bauen und Schlüssel erzeugen =="
(cd "$CLIENT_DIR" && go build -o "$WORK/uucpmail-client" ./cmd/uucpmail-client)
"$WORK/uucpmail-client" keygen >"$WORK/keygen.out"
CFG_DIR="$(dirname "$(find "$HOME" -name id_ed25519.pub | head -1)")"
echo "Config-Verzeichnis: $CFG_DIR"

echo "== Node registrieren (uucpadm; Node-Name wird servergeneriert) =="
cd "$SERVER_DIR"
./scripts/uucpadm user add itest || true
NODE="$(./scripts/uucpadm node add 1 | grep -oE 'u[0-9a-f]{6}' | head -1)"
[ -n "$NODE" ] || { echo "Node-Name nicht ermittelbar"; exit 1; }
echo "Node: $NODE"
./scripts/uucpadm addr add "$ADDR" "$NODE"
./scripts/uucpadm key add "$NODE" "$CFG_DIR/id_ed25519.pub" --comment it
sleep 10   # authorized_keys + sys-Render abwarten (Dev-FAST ~3 s)

echo "== Client-Config schreiben =="
cat > "$CFG_DIR/config.json" <<EOF
{
  "node_name": "$NODE",
  "server_host": "127.0.0.1",
  "server_port": 2222,
  "identities": ["$ADDR"],
  "default_identity": "$ADDR"
}
EOF

echo "== Senden + Sync =="
cd "$CLIENT_DIR"
echo "Integrationstest $(date)" | "$WORK/uucpmail-client" send -subject "IT" ziel@example.com
"$WORK/uucpmail-client" sync
"$WORK/uucpmail-client" queue

echo "== Fertig – extmail-Sink im Stack prüfen (siehe server/tests/test-m5-e2e.sh) =="
