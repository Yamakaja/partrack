#!/usr/bin/env python3
"""
digikey_api.py — call DigiKey 3-legged OAuth endpoints using tokens stored in
the partrack database.

Usage:
    python tools/digikey_api.py [--db-url URL] [--team TEAM_NAME_OR_ID] [endpoint]

Default endpoint: CustomerResource/v1/associatedaccounts

Requires: psycopg2 (pip install psycopg2-binary)
"""

import argparse
import json
import os
import sys
from datetime import datetime, timezone

import urllib.request
import urllib.error

try:
    import psycopg2
    import psycopg2.extras
except ImportError:
    print("ERROR: psycopg2 not installed. Run: pip install psycopg2-binary", file=sys.stderr)
    sys.exit(1)


DEFAULT_DB_URL = os.environ.get(
    "PARTRACK_DB_URL",
    "postgres://partrack:partrack@test-postgres/partrack",
)


def get_token_row(conn, team_filter: str | None):
    """
    Return the first valid DigiKey OAuth token row from the DB, joined with
    the distributor config for client_id / sandbox flag.
    """
    with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        sql = """
            SELECT
                ot.access_token,
                ot.refresh_token,
                ot.expires_at,
                ot.account_id,
                dc.api_credentials,
                t.name  AS team_name,
                t.id    AS team_id,
                u.email AS user_email
            FROM oauth_tokens ot
            JOIN distributor_configs dc
                ON dc.team_id = ot.team_id
               AND dc.distributor_id = ot.distributor_id
            JOIN distributors d
                ON d.id = ot.distributor_id
               AND d.api_type = 'digikey'
            JOIN teams t ON t.id = ot.team_id
            JOIN users u ON u.id = ot.user_id
        """
        params: list = []
        if team_filter:
            sql += """
            WHERE (t.name = %s OR t.id::text = %s)
            """
            params = [team_filter, team_filter]

        sql += " ORDER BY ot.expires_at DESC LIMIT 10"
        cur.execute(sql, params)
        rows = cur.fetchall()

    if not rows:
        return None

    now = datetime.now(timezone.utc)
    # Prefer non-expired tokens; fall back to any row so the caller can attempt a refresh.
    for row in rows:
        if row["expires_at"] > now:
            return row

    print("WARNING: all stored tokens appear expired; using most recent anyway", file=sys.stderr)
    return rows[0]


def base_url(creds: dict) -> str:
    if creds.get("sandbox"):
        return "https://sandbox-api.digikey.com"
    return "https://api.digikey.com"


def refresh_token(creds: dict, refresh_tok: str) -> dict:
    token_url = base_url(creds) + "/v1/oauth2/token"
    data = (
        f"grant_type=refresh_token"
        f"&refresh_token={urllib.parse.quote(refresh_tok)}"
        f"&client_id={urllib.parse.quote(creds['client_id'])}"
        f"&client_secret={urllib.parse.quote(creds['client_secret'])}"
    ).encode()

    req = urllib.request.Request(token_url, data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")

    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read())


def call_endpoint(creds: dict, access_token: str, endpoint_path: str) -> dict:
    url = base_url(creds) + "/" + endpoint_path.lstrip("/")
    req = urllib.request.Request(url, method="GET")
    req.add_header("Authorization", f"Bearer {access_token}")
    req.add_header("X-DIGIKEY-Client-Id", creds["client_id"])
    req.add_header("Accept", "application/json")

    print(f"GET {url}", file=sys.stderr)

    try:
        with urllib.request.urlopen(req) as resp:
            raw = resp.read()
            print(f"HTTP {resp.status}", file=sys.stderr)
            return json.loads(raw)
    except urllib.error.HTTPError as e:
        body = e.read()
        print(f"HTTP {e.code}: {body.decode(errors='replace')}", file=sys.stderr)
        raise


def main():
    import urllib.parse  # needed here for refresh_token

    parser = argparse.ArgumentParser(description="Call DigiKey API using partrack DB tokens")
    parser.add_argument("--db-url", default=DEFAULT_DB_URL, help="PostgreSQL connection URL")
    parser.add_argument("--team", default=None, help="Team name or UUID to filter (optional)")
    parser.add_argument(
        "endpoint",
        nargs="?",
        default="CustomerResource/v1/associatedaccounts",
        help="DigiKey API path (default: CustomerResource/v1/associatedaccounts)",
    )
    args = parser.parse_args()

    conn = psycopg2.connect(args.db_url)
    try:
        row = get_token_row(conn, args.team)
    finally:
        conn.close()

    if row is None:
        print("ERROR: no DigiKey OAuth tokens found in database", file=sys.stderr)
        sys.exit(1)

    creds = row["api_credentials"]  # already a dict via psycopg2 JSONB parsing
    access_token = row["access_token"]

    print(f"Team:     {row['team_name']} ({row['team_id']})", file=sys.stderr)
    print(f"User:     {row['user_email']}", file=sys.stderr)
    print(f"Sandbox:  {creds.get('sandbox', False)}", file=sys.stderr)
    print(f"Expires:  {row['expires_at']}", file=sys.stderr)
    print(f"Account:  {row['account_id'] or '(not yet resolved)'}", file=sys.stderr)

    now = datetime.now(timezone.utc)
    if row["expires_at"] <= now:
        print("Token expired — attempting refresh...", file=sys.stderr)
        tok = refresh_token(creds, row["refresh_token"])
        access_token = tok["access_token"]
        print("Refresh OK", file=sys.stderr)

    result = call_endpoint(creds, access_token, args.endpoint)
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    import urllib.parse  # ensure available for refresh_token at module level too
    main()
