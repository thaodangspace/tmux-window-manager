package store

// DataVersion returns SQLite's PRAGMA data_version for the DB's connection. The
// value is unchanged by writes made on this same connection, but increments
// whenever another connection (e.g. a hook process) commits a change to the
// file. The sidebar loop polls it as a cheap change signal so it only re-reads
// the status rows (store.Live) when an agent actually wrote something.
//
// Reliability note: Open/OpenAt call SetMaxOpenConns(1), so the *sql.DB keeps a
// single underlying connection alive and successive DataVersion reads observe
// the same connection's monotonic counter.
func (db *DB) DataVersion() (int64, error) {
	var v int64
	if err := db.sql.QueryRow("PRAGMA data_version").Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}
