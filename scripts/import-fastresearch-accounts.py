#!/usr/bin/env python3
"""Import FastResearch identity projections without copying local credentials."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
from urllib.parse import parse_qs, unquote, urlsplit


def postgres(psql, statement):
    url = urlsplit(os.environ["FASTCAS_DATABASE_URL"])
    env = dict(
        os.environ,
        PGHOST=url.hostname,
        PGPORT=str(url.port or 5432),
        PGUSER=unquote(url.username),
        PGPASSWORD=unquote(url.password or ""),
        PGDATABASE=unquote(url.path.lstrip("/")),
    )
    for key, values in parse_qs(url.query).items():
        if key in ("host", "sslmode", "sslrootcert", "sslcert", "sslkey"):
            env["PG" + key.upper()] = values[0]
    result = subprocess.run(
        [psql, "-XqAt", "-v", "ON_ERROR_STOP=1"],
        input=statement, encoding="utf-8", stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, env=env, timeout=45,
    )
    if result.returncode:
        # PostgreSQL errors can include row values.
        raise RuntimeError("FastCAS import transaction rejected. Verify schema and existing mappings.")
    return result.stdout.strip()


def plan(db, issuer, client):
    rows = db.execute(
        """SELECT a.id, json_extract(a.content,'$.person'),
                  coalesce(json_extract(a.content,'$.accountDisabledAt'),'') <> '',
                  i.subject
             FROM fr_accounts a LEFT JOIN fr_identities i
               ON i.account_id=a.id AND i.issuer=? AND i.client_id=?
            ORDER BY a.id""",
        (issuer, client),
    ).fetchall()
    if not rows:
        raise RuntimeError("No FastResearch accounts found.")
    result = []
    for account, name, disabled, subject in rows:
        if not account or not isinstance(name, str) or not name.strip() or len(name.strip().encode()) > 200:
            raise RuntimeError("FastResearch contains an invalid account ID or name.")
        digest = hashlib.sha256(account.encode()).hexdigest()
        result.append(dict(
            account=account, name=name.strip(), disabled=bool(disabled),
            current=subject, subject=subject or "fr_" + digest,
            placeholder="fr-" + digest[:40] + "@fastresearch.invalid",
        ))
    legacy = db.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name='fastcas_links'"
    ).fetchone()
    if legacy and db.execute(
        """SELECT count(*) FROM fastcas_links l
            WHERE l.issuer=? AND l.client_id=? AND NOT EXISTS (
                SELECT 1 FROM fr_identities i WHERE i.issuer=l.issuer AND i.client_id=l.client_id
                 AND i.subject=l.subject AND i.account_id=l.account_id)
              AND l.state <> 'revoked'""",
        (issuer, client),
    ).fetchone()[0]:
        raise RuntimeError("An explicit legacy binding needs reconciliation before import.")
    return result


def import_cas(psql, client, accounts, apply):
    def literal(value):
        return "'" + value.replace("'", "''") + "'"

    records = literal(json.dumps(accounts, ensure_ascii=False))
    client = literal(client)
    statement = f"""
        BEGIN;
        SET LOCAL standard_conforming_strings=on;
        SELECT pg_advisory_xact_lock(89004323);
        CREATE TEMP TABLE imported ON COMMIT DROP AS SELECT *
          FROM jsonb_to_recordset({records}::jsonb)
           AS t(account text, name text, disabled boolean, current text, subject text, placeholder text);
        DO $check$
        BEGIN
          IF NOT EXISTS (SELECT FROM applications WHERE id={client} AND active) THEN
            RAISE EXCEPTION 'FastResearch client unavailable';
          END IF;
          IF EXISTS (
            SELECT FROM imported p
              LEFT JOIN identities i ON i.id=p.subject
              LEFT JOIN fastresearch_imports f ON f.local_account_id=p.account
             WHERE (p.current IS NOT NULL AND (i.id IS NULL OR i.deleted_at IS NOT NULL))
                OR (f.identity_id IS NOT NULL AND f.identity_id<>p.subject)
                OR (p.current IS NULL AND f.identity_id IS NULL AND i.id IS NOT NULL)
                OR (p.current IS NULL AND f.identity_id IS NOT NULL AND i.deleted_at IS NOT NULL)
                OR (p.current IS NULL AND f.identity_id IS NULL AND EXISTS (
                    SELECT FROM identities WHERE email=p.placeholder))
                OR EXISTS (SELECT FROM account_links l WHERE l.client_id={client} AND l.state<>'revoked'
                    AND ((l.subject=p.subject AND l.local_ref<>p.account)
                      OR (l.local_ref=p.account AND l.subject<>p.subject)))
          ) THEN RAISE EXCEPTION 'FastResearch account mapping conflict'; END IF;
        END $check$;
        SELECT json_build_object(
          'accounts',count(*),
          'new_cas_identities',count(*) FILTER (WHERE i.id IS NULL),
          'new_local_mappings',count(*) FILTER (WHERE p.current IS NULL))
          FROM imported p LEFT JOIN identities i ON i.id=p.subject;
    """
    if apply:
        statement += """
            INSERT INTO identities(id,email,name,password_hash,role,status)
            SELECT p.subject,p.placeholder,p.name,'','member',CASE WHEN p.disabled THEN 'disabled' ELSE 'active' END
              FROM imported p WHERE p.current IS NULL AND NOT EXISTS (SELECT FROM identities WHERE id=p.subject);
            WITH created AS (
              INSERT INTO fastresearch_imports(local_account_id,identity_id,legacy_name)
              SELECT account,subject,name FROM imported WHERE current IS NULL
              ON CONFLICT (local_account_id) DO NOTHING RETURNING identity_id
            )
            INSERT INTO audit_events(actor,action,target,request_id,result)
            SELECT 'server-maintenance','identity.import.fastresearch',identity_id,'fastresearch-import','success' FROM created;
        """
    output = postgres(psql, statement + ("COMMIT;" if apply else "ROLLBACK;"))
    return json.loads(output.splitlines()[-1])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("database", type=Path, help="active FastResearch accounts.sqlite")
    parser.add_argument("--issuer", required=True)
    parser.add_argument("--client", required=True)
    parser.add_argument("--psql", default="psql")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    uri = args.database.resolve().as_uri() + ("?mode=rw" if args.apply else "?mode=ro")
    db = sqlite3.connect(uri, uri=True, timeout=10, isolation_level=None)
    try:
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("BEGIN IMMEDIATE" if args.apply else "BEGIN")
        accounts = plan(db, args.issuer, args.client)
        result = import_cas(args.psql, args.client, accounts, args.apply)
        if args.apply:
            # FastCAS commits first. A retry uses the same stable subjects if the
            # process stops before SQLite commits or its write needs to be retried.
            db.executemany("INSERT INTO fr_identities VALUES(?,?,?,?)", (
                (args.issuer, args.client, p["subject"], p["account"])
                for p in accounts if p["current"] is None
            ))
            db.commit()
        print(json.dumps(result))
    finally:
        db.close()


if __name__ == "__main__":
    main()
