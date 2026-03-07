#!/bin/bash
# Fetch the full DigiKey category tree and dump as JSON.
#
# Usage:
#   DIGIKEY_CLIENT_ID=... DIGIKEY_CLIENT_SECRET=... ./scripts/digikey-categories.sh > categories.json
#
# Requirements: curl, jq

set -euo pipefail

if [[ -z "${DIGIKEY_CLIENT_ID:-}" || -z "${DIGIKEY_CLIENT_SECRET:-}" ]]; then
  echo "Error: DIGIKEY_CLIENT_ID and DIGIKEY_CLIENT_SECRET must be set" >&2
  exit 1
fi

# Step 1: Get access token
TOKEN=$(curl -sf -X POST "https://api.digikey.com/v1/oauth2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "client_id=${DIGIKEY_CLIENT_ID}&client_secret=${DIGIKEY_CLIENT_SECRET}&grant_type=client_credentials" \
  | jq -r '.access_token')

if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  echo "Error: Failed to obtain access token" >&2
  exit 1
fi

# Step 2: Fetch categories
curl -sf "https://api.digikey.com/products/v4/search/categories" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-DIGIKEY-Client-Id: ${DIGIKEY_CLIENT_ID}" \
  -H "Content-Type: application/json" \
  | jq '.'
