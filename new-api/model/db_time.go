package model

import (
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

// GetDBTimestamp returns a UNIX timestamp from database time.
// Falls back to application time on error.
func GetDBTimestamp() int64 {
	return getDBTimestampDB(DB)
}

// getDBTimestampTx returns a DB-sourced timestamp bound to the given tx. Use
// this inside a DB.Transaction closure: on a single-connection pool (e.g. the
// test in-memory SQLite with SetMaxOpenConns(1)), calling GetDBTimestamp would
// block forever waiting for the conn the tx already holds.
func getDBTimestampTx(tx *gorm.DB) int64 {
	return getDBTimestampDB(tx)
}

func getDBTimestampDB(db *gorm.DB) int64 {
	var ts int64
	var err error
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		err = db.Raw("SELECT EXTRACT(EPOCH FROM NOW())::bigint").Scan(&ts).Error
	case common.UsingMainDatabase(common.DatabaseTypeSQLite):
		err = db.Raw("SELECT strftime('%s','now')").Scan(&ts).Error
	default:
		err = db.Raw("SELECT UNIX_TIMESTAMP()").Scan(&ts).Error
	}
	if err != nil || ts <= 0 {
		return common.GetTimestamp()
	}
	return ts
}
