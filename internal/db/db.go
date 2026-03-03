package db

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	Conn        *sql.DB
	IsEphemeral bool
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

	d := &DB{Conn: conn, IsEphemeral: false}
	if err := d.Migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return d, nil
}

func OpenInMem() (*DB, error) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	d := &DB{Conn: conn, IsEphemeral: true}
	if err := d.Migrate(); err != nil {
		return nil, err
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

func (d *DB) SeedDummyData() error {
	now := time.Now()
	for i := 21; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")

		// Blood pressure: (90-140)/(80-90) - pulse (80-120)
		sys := 90 + rand.Intn(51)
		dia := 80 + rand.Intn(11)
		pulse := 80 + rand.Intn(41)
		d.LogMetric("bp", fmt.Sprintf("%d/%d - %d", sys, dia, pulse), date)

		// Alcohol: 2-3 days consecutive, twice in dataset
		// Simplified: specific days
		if (i >= 5 && i <= 7) || (i >= 15 && i <= 16) {
			d.LogMetric("alcohol", "true", date)
		}

		// Hydration: mostly normal, 4 days low
		hydra := "Normal"
		if i == 3 || i == 10 || i == 14 || i == 20 {
			hydra = "Low"
		}
		d.LogMetric("hydration", hydra, date)

		// Sleep: (5-8h):(0-59m)
		h := 5 + rand.Intn(4)
		m := rand.Intn(60)
		d.LogMetric("sleep", fmt.Sprintf("%02d:%02d", h, m), date)

		// Training: every 3-4 days
		if i%4 == 0 {
			d.LogMetric("training", "true", date)
		}

		// Stress: 1-5
		d.LogMetric("stress", fmt.Sprintf("%d", 1+rand.Intn(5)), date)

		// Feel: 1-5
		d.LogMetric("feel", fmt.Sprintf("%d", 1+rand.Intn(5)), date)
	}
	return nil
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

type DBStats struct {
	TotalEntries int
	FirstEntry   string
	LastEntry    string
	MetricCounts map[string]int
}

func (d *DB) GetStats() (*DBStats, error) {
	stats := &DBStats{
		MetricCounts: make(map[string]int),
	}

	err := d.Conn.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&stats.TotalEntries)
	if err != nil {
		return nil, err
	}

	if stats.TotalEntries > 0 {
		err = d.Conn.QueryRow("SELECT MIN(date), MAX(date) FROM metrics").Scan(&stats.FirstEntry, &stats.LastEntry)
		if err != nil {
			return nil, err
		}

		rows, err := d.Conn.Query("SELECT metric_type, COUNT(*) FROM metrics GROUP BY metric_type")
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var mType string
			var count int
			if err := rows.Scan(&mType, &count); err == nil {
				stats.MetricCounts[mType] = count
			}
		}
	}

	return stats, nil
}

func (d *DB) Reset() error {
	_, err := d.Conn.Exec("DELETE FROM metrics")
	return err
}
