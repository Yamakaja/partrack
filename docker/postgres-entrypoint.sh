#!/bin/sh
set -e

PGDATA=/var/lib/postgres/data

if [ ! -f "$PGDATA/PG_VERSION" ]; then
    initdb -D "$PGDATA" --auth=trust

    # Apply pg_hba so password auth works from other containers
    cat >> "$PGDATA/pg_hba.conf" <<EOF
host all all 0.0.0.0/0 scram-sha-256
EOF

    echo "listen_addresses='*'" >> "$PGDATA/postgresql.conf"

    # Start temporarily to create user and database
    pg_ctl -D "$PGDATA" -w start

    if [ -n "$POSTGRES_USER" ] && [ "$POSTGRES_USER" != "postgres" ]; then
        psql -v ON_ERROR_STOP=1 -c "CREATE USER $POSTGRES_USER WITH PASSWORD '$POSTGRES_PASSWORD';"
    fi

    if [ -n "$POSTGRES_DB" ]; then
        psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE $POSTGRES_DB OWNER ${POSTGRES_USER:-postgres};"
        psql -v ON_ERROR_STOP=1 -d "$POSTGRES_DB" -c "CREATE EXTENSION IF NOT EXISTS pg_uuidv7;"
    fi

    pg_ctl -D "$PGDATA" -w stop
fi

exec "$@"
