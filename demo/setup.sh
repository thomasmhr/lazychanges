#!/usr/bin/env bash
# Builds the fictional workspace the demo records: a meta-repository holding two apps,
# with some work already in progress.
set -euo pipefail
root=${1:?usage: setup.sh <dir>}
rm -rf "$root" && mkdir -p "$root"
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com
export GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com

init() { git -C "$1" init -q -b main && git -C "$1" add -A && git -C "$1" commit -qm init; }
put() { mkdir -p "$(dirname "$1")" && cat > "$1"; }

cd "$root"
put .gitignore <<<'/apps/*/'
put README.md <<'MD'
# shop

The workspace of the shop: `apps/api` serves orders, `apps/web` is the storefront.
MD
put docs/architecture.md <<'MD'
# Architecture

The storefront calls the API over HTTP. Orders are stored in Postgres.
Payments go through the payment provider; refunds are handled by hand.
MD
init .

put apps/api/go.mod <<<'module example.com/shop/api'
put apps/api/handlers/orders.go <<'GO'
package handlers

import "net/http"

func ListOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := store.Orders(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, orders)
}
GO
put apps/api/handlers/legacy_export.go <<'GO'
package handlers

// ExportCSV is kept for the old back office.
func ExportCSV() {}
GO
init apps/api

put apps/web/package.json <<<'{ "name": "storefront" }'
put apps/web/src/cart.ts <<'TS'
export function total(items: Item[]): number {
  return items.reduce((sum, item) => sum + item.price, 0)
}
TS
init apps/web

# Work already in progress when the session starts.
put apps/api/handlers/orders.go <<'GO'
package handlers

import "net/http"

func ListOrders(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	orders, err := store.Orders(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, orders)
}
GO
