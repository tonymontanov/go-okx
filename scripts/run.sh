#!/usr/bin/env bash
# ----------------------------------------------------------------------------
# scripts/run.sh
#
# Wrapper для запуска любого примера go-okx с переменными из .env.
#
# Использование:
#   ./scripts/run.sh ./examples/simple-trade
#   ./scripts/run.sh ./examples/orderbook-watcher
#
# Поведение:
#   - Загружает все переменные из .env (если он есть рядом с этим скриптом).
#   - Падает с понятной ошибкой, если .env отсутствует.
#   - Передаёт все аргументы как есть в `go run`.
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
