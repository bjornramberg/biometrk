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
	Text        string
	Correlation float64
	IsLagged    bool
}

func (d *DB) GetInsights(days int) ([]Insight, error) {
	data, err := d.GetMetricDataInRange(days)
	if err != nil {
		return nil, err
	}

	metrics := []string{"bp", "alcohol", "hydration", "sleep", "training", "stress", "feel", "note"}
	labels := map[string]string{
		"bp":        "Blood Pressure",
		"alcohol":   "Alcohol Intake",
		"hydration": "Hydration",
		"sleep":     "Sleep",
		"training":  "Training",
		"stress":    "Stress",
		"feel":      "Overall Feel",
	}

	var insights []Insight

	for i := 0; i < len(metrics); i++ {
		for j := i + 1; j < len(metrics); j++ {
			m1, m2 := metrics[i], metrics[j]
			d1, d2 := data[m1], data[m2]

			if len(d1) < 3 || len(d2) < 3 {
				continue
			}

			r := calculatePearson(d1, d2, m1, m2)
			
			if r > 0.4 || r < -0.4 {
				text := generateInsightText(labels[m1], labels[m2], r)
				insights = append(insights, Insight{Text: text, Correlation: r})
			}
		}
	}

	return insights, nil
}

func calculatePearson(x, y []float64, id1, id2 string) float64 {
	// Identify metrics where 0 means "missing data" (numeric) 
	// vs where 0 is a valid state (boolean/enum)
	isNumeric := func(id string) bool {
		return id == "bp" || id == "sleep" || id == "stress" || id == "feel"
	}

	var cleanX, cleanY []float64
	for i := 0; i < len(x); i++ {
		// Skip if numeric data is 0 (missing)
		if isNumeric(id1) && x[i] == 0 { continue }
		if isNumeric(id2) && y[i] == 0 { continue }
		
		cleanX = append(cleanX, x[i])
		cleanY = append(cleanY, y[i])
	}

	n := len(cleanX)
	if n < 5 { // Require at least 5 shared data points for any correlation
		return 0
	}

	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := 0; i < n; i++ {
		sumX += cleanX[i]
		sumY += cleanY[i]
		sumXY += cleanX[i] * cleanY[i]
		sumX2 += cleanX[i] * cleanX[i]
		sumY2 += cleanY[i] * cleanY[i]
	}

	num := float64(n)*sumXY - sumX*sumY
	den := (float64(n)*sumX2 - sumX*sumX) * (float64(n)*sumY2 - sumY*sumY)
	if den <= 0 {
		return 0
	}

	return num / math.Sqrt(den)
}

func generateInsightText(l1, l2 string, r float64) string {
	strength := "a moderate"
	if r > 0.7 || r < -0.7 {
		strength = "a strong"
	}

	direction := "positive"
	impact := "increase together"
	if r < 0 {
		direction = "negative"
		impact = "move in opposite directions"
	}

	if (l1 == "Training" || l2 == "Training") && r > 0.4 {
		return fmt.Sprintf("Training consistently correlates with a better %s.", strings.ReplaceAll(l1+l2, "Training", ""))
	}
	if (l1 == "Alcohol Intake" || l2 == "Alcohol Intake") && r < -0.4 {
		return fmt.Sprintf("Alcohol intake shows %s link to decreased %s.", strength, strings.ReplaceAll(l1+l2, "Alcohol Intake", ""))
	}

	return fmt.Sprintf("There is %s %s correlation between %s and %s (%s).", strength, direction, l1, l2, impact)
}

func (d *DB) GetLeadLagInsights(days int) ([]Insight, error) {
	data, err := d.GetMetricDataInRange(days + 1)
	if err != nil {
		return nil, err
	}

	metrics := []string{"bp", "alcohol", "hydration", "sleep", "training", "stress", "feel", "note"}
	labels := map[string]string{
		"bp":        "Blood Pressure",
		"alcohol":   "Alcohol Intake",
		"hydration": "Hydration",
		"sleep":     "Sleep",
		"training":  "Training",
		"stress":    "Stress",
		"feel":      "Overall Feel",
	}

	var insights []Insight

	for _, m1 := range metrics {
		for _, m2 := range metrics {
			d1 := data[m1]
			d2 := data[m2]

			if len(d1) < 4 || len(d2) < 4 {
				continue
			}

			yesterdayD1 := d1[:len(d1)-1]
			todayD2 := d2[1:]

			r := calculatePearson(yesterdayD1, todayD2, m1, m2)

			if r > 0.4 || r < -0.4 {
				text := fmt.Sprintf("Yesterday's %s shows a correlation with today's %s.", labels[m1], labels[m2])
				if r > 0.7 {
					text = fmt.Sprintf("Yesterday's %s strongly impacts how today's %s turns out.", labels[m1], labels[m2])
				}
				insights = append(insights, Insight{Text: text, Correlation: r, IsLagged: true})
			}
		}
	}

	return insights, nil
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
