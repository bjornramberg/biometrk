package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	Conn *sql.DB
}

func Open() (*DB, error) {
	// For now, use a local file in the project root
	dbPath := "biometrk.db"
	
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	d := &DB{Conn: conn}
	if err := d.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return d, nil
}

func (d *DB) Migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date TEXT NOT NULL DEFAULT (date('now')),
		metric_type TEXT NOT NULL,
		value TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_metrics_date ON metrics(date);
	`
	_, err := d.Conn.Exec(query)
	return err
}

func (d *DB) Close() error {
	return d.Conn.Close()
}

func (d *DB) LogMetric(metricType, value, date string) error {
	query := `INSERT INTO metrics (metric_type, value, date) VALUES (?, ?, ?)`
	_, err := d.Conn.Exec(query, metricType, value, date)
	return err
}

func (d *DB) GetMetricsByDate(date string) ([]string, error) {
	query := `SELECT metric_type FROM metrics WHERE date = ?`
	rows, err := d.Conn.Query(query, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []string
	for rows.Next() {
		var metric string
		if err := rows.Scan(&metric); err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

func (d *DB) DeleteMetric(metricType, date string) error {
	query := `DELETE FROM metrics WHERE metric_type = ? AND date = ?`
	_, err := d.Conn.Exec(query, metricType, date)
	return err
}
