package db

import (
	"context"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(dsn string) (*gorm.DB, error) {
	return gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	})
}

// StartTTLCleanup launches a background goroutine that periodically deletes
// expired rows from tables with an expires_at column. Replaces Mongo's TTL
// index. Stops when ctx is cancelled.
func StartTTLCleanup(ctx context.Context, db *gorm.DB) {
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweepExpired(db)
			}
		}
	}()
}

func sweepExpired(db *gorm.DB) {
	for _, table := range []string{"user_sessions", "tokens"} {
		res := db.Exec("DELETE FROM "+table+" WHERE expires_at < ?", time.Now())
		if res.Error != nil {
			log.Printf("ttl sweep %s: %v", table, res.Error)
		}
	}
}
