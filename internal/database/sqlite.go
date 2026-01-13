package database

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func NewSQLiteDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	if err := db.AutoMigrate(&Release{}, &ReleaseRepo{}, &User{}, &ReleaseHistory{}); err != nil {
		return nil, fmt.Errorf("auto migrating: %w", err)
	}

	return db, nil
}
