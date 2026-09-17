#!/bin/sh
# Brings up a genuine streaming replica: on an empty data directory it takes a
# base backup from the primary and starts as a hot standby. This is deliberately
# not a fake second database. The read/write routing, the replication lag checks
# and the read-your-writes fallback are only meaningfully exercised against real
# replication.
set -e

# The container starts as root so that it can take ownership of a freshly
# created volume, exactly as the stock entrypoint does, then drops to postgres
# for anything that touches the data directory.
if [ "$(id -u)" = '0' ]; then
    mkdir -p "$PGDATA"
    chown -R postgres:postgres /var/lib/postgresql
    chmod 0750 "$PGDATA"
fi

if [ ! -s "$PGDATA/PG_VERSION" ]; then
    echo "replica: waiting for primary at $PRIMARY_HOST"
    until pg_isready -h "$PRIMARY_HOST" -U "$REPLICATION_USER" -q; do
        sleep 1
    done

    # The replication slot makes the primary retain WAL while this replica is
    # offline. Created separately and idempotently, because a slot can outlive
    # the replica's data volume and pg_basebackup --create-slot then fails.
    echo "replica: ensuring replication slot $REPLICATION_SLOT"
    gosu postgres env PGPASSWORD="$REPLICATION_PASSWORD" psql \
        --host="$PRIMARY_HOST" --username="$REPLICATION_USER" --dbname=postgres \
        --no-password --quiet --tuples-only <<-SLOT
	SELECT pg_create_physical_replication_slot('$REPLICATION_SLOT')
	WHERE NOT EXISTS (
	    SELECT 1 FROM pg_replication_slots WHERE slot_name = '$REPLICATION_SLOT'
	);
	SLOT

    echo "replica: taking base backup from primary"
    rm -rf "${PGDATA:?}"/*
    # -R writes the standby configuration pointing back at the primary.
    gosu postgres env PGPASSWORD="$REPLICATION_PASSWORD" pg_basebackup \
        --host="$PRIMARY_HOST" \
        --username="$REPLICATION_USER" \
        --pgdata="$PGDATA" \
        --wal-method=stream \
        --slot="$REPLICATION_SLOT" \
        --write-recovery-conf \
        --progress --no-password
    chmod 0750 "$PGDATA"
    echo "replica: base backup complete"
fi

exec docker-entrypoint.sh postgres
