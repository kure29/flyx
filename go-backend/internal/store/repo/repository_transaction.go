package repo

import "gorm.io/gorm"

func (r *Repository) Transaction(fn func(*Repository) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return fn(&Repository{db: tx, dbPath: r.dbPath})
	})
}
