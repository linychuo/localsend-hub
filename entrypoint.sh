#!/bin/sh
# ==========================================
# LocalSend Hub - Docker Entrypoint
# ==========================================
# Configuration priority: Env Vars > Config File > Defaults
#
# Environment Variables (override everything):
#   LOCALSEND_RECEIVE_DIR  - File receive directory
#   LOCALSEND_PORT         - Core HTTPS port (default: 53317)
#   LOCALSEND_ADMIN_PORT   - Admin panel port (default: 53318)
#   LOCALSEND_DEVICE_NAME  - Device alias (default: LocalSend Hub)
#   LOCALSEND_DEVICE_TYPE  - Device type (default: server)
#   LOCALSEND_MAX_LOGS     - Max log entries (default: 1000)
#
# Config File (/app/config/localsend_config.json):
#   Used when env vars are NOT set.
#   Admin UI changes are persisted here.
# ==========================================

set -e

# Ensure required directories exist
mkdir -p /app/config
mkdir -p "${LOCALSEND_RECEIVE_DIR:-/app/received}"

echo "🚀 LocalSend Hub Starting (Separated Services)..."
echo "   📝 Config Priority: Env Vars > Config File > Defaults"

# Start Core Service (background process)
echo "🌐 Starting Core Service on port ${LOCALSEND_PORT:-53317}..."
/app/localsend-hub &
CORE_PID=$!

# Start Admin Service (background process)
echo "🛡️ Starting Admin Service on port ${LOCALSEND_ADMIN_PORT:-53318}..."
/app/localsend-hub-admin &
ADMIN_PID=$!

# Wait for any process to exit
wait -n $CORE_PID $ADMIN_PID 2>/dev/null || true

# If core service exited, log and exit (admin can continue but we want both running)
if ! kill -0 $CORE_PID 2>/dev/null; then
    echo "❌ Core Service exited unexpectedly"
    kill $ADMIN_PID 2>/dev/null || true
    exit 1
fi

# If admin service exited, log but core continues
if ! kill -0 $ADMIN_PID 2>/dev/null; then
    echo "⚠️ Admin Service exited, Core Service continues"
    wait $CORE_PID
    exit $?
fi

# Both services still running, wait for either to exit
wait
