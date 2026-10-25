#!/usr/bin/env bash
# Plays a coding agent at work in the demo workspace, one edit every few seconds.
set -euo pipefail
cd "${1:?usage: agent.sh <dir>}"
put() { mkdir -p "$(dirname "$1")" && cat > "$1"; }

sleep 4
put apps/api/handlers/refunds.go <<'GO'
package handlers

import "net/http"

// Refund gives an order's payment back to the customer.
func Refund(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := payments.Refund(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
GO
sleep 3
put apps/web/src/cart.ts <<'TS'
export function total(items: Item[], coupon?: Coupon): number {
  const sum = items.reduce((sum, item) => sum + item.price * item.quantity, 0)
  return coupon ? sum * (1 - coupon.rate) : sum
}
TS
sleep 3
rm apps/api/handlers/legacy_export.go
put docs/architecture.md <<'MD'
# Architecture

The storefront calls the API over HTTP. Orders are stored in Postgres.
Payments go through the payment provider; refunds go through `POST /orders/{id}/refund`.
MD
