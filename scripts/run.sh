#!/usr/bin/env bash
# ----------------------------------------------------------------------------
# scripts/run.sh
#
# Wrapper for running any go-okx example with variables from .env.
#
# Usage:
#   ./scripts/run.sh ./examples/simple-trade
#   ./scripts/run.sh ./examples/orderbook-watcher
#
# Behavior:
#   - Loads all variables from .env (if present next to this script).
#   - Exits with a clear error if .env is missing.
#   - Passes all arguments as-is to `go run`.
# ----------------------------------------------------------------------------

set -euo pipefail

readonly ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ENV_FILE="${ROOT_DIR}/.env"

if [[ ! -f "${ENV_FILE}" ]]; then
    echo "error: ${ENV_FILE} not found." >&2
    echo "       cp .env.example .env  &&  edit it with your keys" >&2
    exit 1
fi

if [[ $# -lt 1 ]]; then
    echo "usage: $0 <go-package-path>" >&2
    echo "  e.g.: $0 ./examples/simple-trade" >&2
    exit 1
fi

set -a
# shellcheck disable=SC1090
source "${ENV_FILE}"
set +a

cd "${ROOT_DIR}"
exec go run "$@"
