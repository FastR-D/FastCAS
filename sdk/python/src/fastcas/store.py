from __future__ import annotations

import json
import os
import sqlite3
import time
from pathlib import Path
from typing import Protocol


class TransactionStore(Protocol):
    def put(self, transaction: dict) -> None: ...
    def take(self, state: str) -> dict | None:
        """Atomically consume a transaction. Concurrent calls cannot both return it."""
        ...


class SQLiteTransactionStore:
    """Single-host persistent store, usable from multiple worker processes.

    For multi-host deployments inject a shared transactional database adapter.
    The dedicated database stores PKCE verifiers, so protect its directory too.
    """

    def __init__(self, path: str | Path):
        self.path = Path(path)
        self.path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        fd = os.open(self.path, os.O_CREAT | os.O_RDWR, 0o600)
        os.close(fd)
        if self.path.stat().st_mode & 0o077:
            raise ValueError("Transaction database must have private file permissions")
        with self._connect() as db:
            db.execute("CREATE TABLE IF NOT EXISTS fastcas_transactions(state TEXT PRIMARY KEY, payload TEXT NOT NULL, expires REAL NOT NULL)")

    def _connect(self):
        return sqlite3.connect(str(self.path), timeout=10)

    def put(self, transaction: dict) -> None:
        with self._connect() as db:
            db.execute("DELETE FROM fastcas_transactions WHERE expires < ?", (time.time(),))
            db.execute("INSERT INTO fastcas_transactions VALUES(?,?,?)", (transaction["state"], json.dumps(transaction), transaction["expires_at"]))

    def take(self, state: str) -> dict | None:
        with self._connect() as db:
            db.execute("BEGIN IMMEDIATE")
            row = db.execute("SELECT payload FROM fastcas_transactions WHERE state=?", (state,)).fetchone()
            db.execute("DELETE FROM fastcas_transactions WHERE state=?", (state,))
            return json.loads(row[0]) if row else None
