import concurrent.futures
import tempfile
import time
import unittest
from pathlib import Path

from fastcas import Configuration, FastCAS, FastCASError, SQLiteTransactionStore


class StoreTests(unittest.TestCase):
    def test_transaction_is_consumed_once_across_connections(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/"transactions.sqlite"
            store = SQLiteTransactionStore(path)
            store.put(dict(state="one", expires_at=time.time()+300))
            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
                results = list(executor.map(lambda _: SQLiteTransactionStore(path).take("one"), range(8)))
            self.assertEqual(sum(result is not None for result in results), 1)

    def test_insecure_remote_configuration_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FastCASError):
                FastCAS(Configuration("http://remote.test", "app", "http://localhost/callback", allow_loopback_http=True), SQLiteTransactionStore(Path(directory)/"transactions.sqlite"))


if __name__ == "__main__":
    unittest.main()
