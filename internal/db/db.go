package db

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	metricType = strings.ToLower(metricType)
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

		// Note
		notes := []string{"Feeling great today.", "A bit tired.", "Productive morning.", "Stayed hydrated.", "Long day at work.", ""}
		d.LogMetric("note", notes[rand.Intn(len(notes))], date)
	}
	return nil
}

func (d *DB) GetMetricValueOnDate(metricType, date string) (string, error) {
	query := `SELECT value FROM metrics WHERE metric_type = ? AND date = ?`
	var value string
	err := d.Conn.QueryRow(query, metricType, date).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (d *DB) DeleteMetric(metricType, date string) error {
	metricType = strings.ToLower(metricType)
	query := `DELETE FROM metrics WHERE metric_type = ? AND date = ?`
	_, err := d.Conn.Exec(query, metricType, date)
	return err
}

type DBStats struct {
	TotalEntries  int
	FirstEntry    string
	LastEntry     string
	MetricCounts  map[string]int
	LongestStreak int
	Path          string
	Size          int64
}

func (d *DB) GetStats() (*DBStats, error) {
	stats := &DBStats{
		MetricCounts: make(map[string]int),
		Path:         "biometrk.db",
	}

	if !d.IsEphemeral {
		fi, err := os.Stat(stats.Path)
		if err == nil {
			stats.Size = fi.Size()
		}
	} else {
		stats.Path = ":memory:"
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

		longest, _ := d.CalculateLongestStreak()
		stats.LongestStreak = longest
	}

	return stats, nil
}

func (d *DB) CalculateLongestStreak() (int, error) {
	query := `SELECT DISTINCT date FROM metrics ORDER BY date ASC`
	rows, err := d.Conn.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	maxStreak := 0
	currentStreak := 0
	var lastDate time.Time

	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err != nil {
			return maxStreak, err
		}

		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		if currentStreak == 0 {
			currentStreak = 1
		} else {
			if date.Equal(lastDate.AddDate(0, 0, 1)) {
				currentStreak++
			} else {
				if currentStreak > maxStreak {
					maxStreak = currentStreak
				}
				currentStreak = 1
			}
		}
		lastDate = date
	}

	if currentStreak > maxStreak {
		maxStreak = currentStreak
	}

	return maxStreak, nil
}

func (d *DB) Reset() error {
	_, err := d.Conn.Exec("DELETE FROM metrics")
	return err
}

func (d *DB) GetStreak() (int, error) {
	query := `SELECT DISTINCT date FROM metrics ORDER BY date DESC`
	rows, err := d.Conn.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	dates := make(map[string]bool)
	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err == nil {
			dates[dateStr] = true
		}
	}

	streak := 0
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	
	// Start checking from today
	checkDate := today
	
	// If today isn't logged, check if yesterday was. 
	// If neither, streak is 0.
	if !dates[checkDate.Format("2006-01-02")] {
		checkDate = today.AddDate(0, 0, -1)
		if !dates[checkDate.Format("2006-01-02")] {
			return 0, nil
		}
	}

	// Count backwards
	for {
		if dates[checkDate.Format("2006-01-02")] {
			streak++
			checkDate = checkDate.AddDate(0, 0, -1)
		} else {
			break
		}
	}

	return streak, nil
}

func (d *DB) Backup() (string, error) {
	if d.IsEphemeral {
		return "", fmt.Errorf("cannot backup in-memory database")
	}

	backupDir := "backups"
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		os.Mkdir(backupDir, 0755)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("biometrk-backup-%s.db", timestamp))

	input, err := os.ReadFile("biometrk.db")
	if err != nil {
		return "", err
	}

	err = os.WriteFile(backupPath, input, 0644)
	if err != nil {
		return "", err
	}

	return backupPath, nil
}

func (d *DB) Restore(backupPath string) error {
	if d.IsEphemeral {
		return fmt.Errorf("cannot restore to in-memory database")
	}

	// Close current connection
	d.Conn.Close()

	// Copy backup over main db
	input, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}

	err = os.WriteFile("biometrk.db", input, 0644)
	if err != nil {
		return err
	}

	// Reopen
	conn, err := sql.Open("sqlite", "biometrk.db")
	if err != nil {
		return err
	}
	d.Conn = conn
	return nil
}

func (d *DB) ExportCSV() (string, error) {
	exportsDir := "exports"
	if _, err := os.Stat(exportsDir); os.IsNotExist(err) {
		os.Mkdir(exportsDir, 0755)
	}

	filename := filepath.Join(exportsDir, fmt.Sprintf("biometrk-export-%s.csv", time.Now().Format("20060102-150405")))
	f, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	rows, err := d.Conn.Query("SELECT date, metric_type, value FROM metrics ORDER BY date DESC, metric_type ASC")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	f.WriteString("date,metric,value\n")
	for rows.Next() {
		var date, mType, val string
		if err := rows.Scan(&date, &mType, &val); err == nil {
			f.WriteString(fmt.Sprintf("%s,%s,%s\n", date, mType, val))
		}
	}

	return filename, nil
}

func (d *DB) ExportMarkdown() (string, error) {
	exportsDir := "exports"
	if _, err := os.Stat(exportsDir); os.IsNotExist(err) {
		os.Mkdir(exportsDir, 0755)
	}

	filename := filepath.Join(exportsDir, fmt.Sprintf("biometrk-report-%s.md", time.Now().Format("20060102-150405")))
	f, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	rows, err := d.Conn.Query("SELECT date, metric_type, value FROM metrics ORDER BY date DESC, metric_type ASC")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	f.WriteString("# Biometrk Health Report\n\n")
	f.WriteString("| Date | Metric | Value |\n")
	f.WriteString("| --- | --- | --- |\n")
	for rows.Next() {
		var date, mType, val string
		if err := rows.Scan(&date, &mType, &val); err == nil {
			f.WriteString(fmt.Sprintf("| %s | %s | %s |\n", date, mType, val))
		}
	}

	return filename, nil
}

type Insight struct {
	Text   string
	Impact string // e.g., "+1.2 points" or "-10 mmHg"
	Trend  int    // 1 for positive impact, -1 for negative, 0 for neutral
}

type causalPair struct {
	cause  string
	effect string
	label  string
	isLag  bool
}

func (d *DB) GetSmartInsights(days int) ([]Insight, error) {
	data, err := d.GetMetricDataInRange(days + 1)
	if err != nil {
		return nil, err
	}

	pairs := []causalPair{
		{cause: "alcohol", effect: "sleep", label: "Alcohol's impact on your Sleep", isLag: false},
		{cause: "alcohol", effect: "feel", label: "How Alcohol affects your Well-being", isLag: true},
		{cause: "sleep", effect: "stress", label: "How Sleep affects your Stress levels", isLag: false},
		{cause: "sleep", effect: "feel", label: "The link between Sleep and Well-being", isLag: false},
		{cause: "training", effect: "feel", label: "The boost you get from Training", isLag: false},
		{cause: "stress", effect: "bp", label: "How Stress impacts your Blood Pressure", isLag: false},
		{cause: "hydration", effect: "feel", label: "The benefit of staying Hydrated", isLag: false},
	}

	var insights []Insight

	for _, p := range pairs {
		causeData := data[p.cause]
		effectData := data[p.effect]

		if len(causeData) < 5 || len(effectData) < 5 {
			continue
		}

		var cause, effect []float64
		if p.isLag {
			// Yesterday's cause vs Today's effect
			cause = causeData[:len(causeData)-1]
			effect = effectData[1:]
		} else {
			cause = causeData[1:] // Use the same range as lag for consistency
			effect = effectData[1:]
		}

		insight, ok := d.analyzeCausalRelationship(p, cause, effect)
		if ok {
			insights = append(insights, insight)
		}
	}

	return insights, nil
}

func (d *DB) analyzeCausalRelationship(p causalPair, cause, effect []float64) (Insight, bool) {
	// For boolean/toggle causes (Alcohol, Training, Hydration)
	// We compare the average of the effect when cause is 1 vs when cause is 0
	var sumOn, sumOff float64
	var countOn, countOff int

	for i := 0; i < len(cause); i++ {
		if cause[i] > 0.5 {
			if effect[i] > 0 {
				sumOn += effect[i]
				countOn++
			}
		} else {
			if effect[i] > 0 {
				sumOff += effect[i]
				countOff++
			}
		}
	}

	if countOn < 2 || countOff < 2 {
		return Insight{}, false
	}

	avgOn := sumOn / float64(countOn)
	avgOff := sumOff / float64(countOff)
	diff := avgOn - avgOff

	// Significant threshold check
	if math.Abs(diff) < 0.2 {
		return Insight{}, false
	}

	// Generate Text
	text := ""
	trend := 0
	impact := ""

	switch p.cause {
	case "alcohol":
		if diff < 0 {
			text = fmt.Sprintf("Your %s is consistently lower after consuming alcohol.", p.effect)
			trend = -1
		}
	case "training":
		if diff > 0 {
			text = fmt.Sprintf("Training days correlate with a boost in your %s.", p.effect)
			trend = 1
		}
	case "sleep":
		if diff > 0 {
			text = fmt.Sprintf("Better sleep leads to a noticeable improvement in %s.", p.effect)
			trend = 1
		} else {
			text = fmt.Sprintf("Lack of sleep seems to increase your %s.", p.effect)
			trend = -1
		}
	}

	if text == "" {
		// Generic fallback
		if diff > 0 {
			text = fmt.Sprintf("There is a positive link between %s and %s.", p.cause, p.effect)
			trend = 1
		} else {
			text = fmt.Sprintf("%s seems to have a negative impact on %s.", p.cause, p.effect)
			trend = -1
		}
	}

	// Format impact
	unit := "points"
	if p.effect == "bp" {
		unit = "mmHg"
	} else if p.effect == "sleep" {
		unit = "hours"
	}
	impact = fmt.Sprintf("%+.1f %s", diff, unit)

	return Insight{Text: text, Impact: impact, Trend: trend}, true
}

func (d *DB) GetMetricDataInRange(days int) (map[string][]float64, error) {
	data := make(map[string][]float64)
	
	metricsList := []string{"bp", "alcohol", "hydration", "sleep", "training", "stress", "feel", "note"}
	for _, m := range metricsList {
		data[m] = make([]float64, 0, days)
	}

	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -days+1).Format("2006-01-02")

	query := `SELECT date, metric_type, value FROM metrics WHERE date BETWEEN ? AND ? ORDER BY date ASC`
	rows, err := d.Conn.Query(query, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tempData := make(map[string]map[string]float64)
	for rows.Next() {
		var dStr, mType, valStr string
		if err := rows.Scan(&dStr, &mType, &valStr); err == nil {
			if _, ok := tempData[dStr]; !ok {
				tempData[dStr] = make(map[string]float64)
			}

			var val float64
			if mType == "bp" {
				parts := strings.Split(valStr, "/")
				if len(parts) > 0 {
					if f, err := strconv.ParseFloat(parts[0], 64); err == nil {
						val = f
					}
				}
			} else if mType == "sleep" {
				parts := strings.Split(valStr, ":")
				if len(parts) == 2 {
					h, _ := strconv.ParseFloat(parts[0], 64)
					m, _ := strconv.ParseFloat(parts[1], 64)
					val = h + (m / 60.0)
				}
			} else if valStr == "true" {
				val = 1
			} else if valStr == "false" {
				val = 0
			} else if valStr == "Normal" {
				val = 1
			} else if valStr == "Low" {
				val = 0
			} else {
				if f, err := strconv.ParseFloat(valStr, 64); err == nil {
					val = f
				}
			}
			tempData[dStr][mType] = val
		}
	}

	for i := 0; i < days; i++ {
		dStr := time.Now().AddDate(0, 0, -days+1+i).Format("2006-01-02")
		dayMetrics := tempData[dStr]

		for _, mType := range metricsList {
			val := 0.0
			if dayMetrics != nil {
				if v, ok := dayMetrics[mType]; ok {
					val = v
				}
			}
			data[mType] = append(data[mType], val)
		}
	}

	return data, nil
}
