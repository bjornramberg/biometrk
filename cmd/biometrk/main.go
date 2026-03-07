package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/bjornramberg/biometrk/internal/db"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/guptarohit/asciigraph"
	)

	type metricType int

	const (
	typeToggle metricType = iota
	typeInput
	typeEnum
	typeRating
	)

	type metricDefinition struct {
	id          string
	label       string
	tooltip     string
	mType       metricType
	placeholder string
	validate    func(string) bool
	options     []string // For typeEnum
	}

	type viewMode int

	const (
	modeTracker viewMode = iota
	modeDatabase
	modeAnalytics
	)

	type model struct {
	db                *db.DB
	metrics           []metricDefinition
	values            map[string]string // metricID -> value
	cursor            int
	currentDate       time.Time
	err               error
	input             textinput.Model
	isInputting       bool
	inputStep         int
	tempValues        []string
	mode              viewMode
	dbStats           *db.DBStats
	streak            int
	analyticsInterval int // 7, 30, 90
	analyticsData     map[string][]float64
	analyticsInsights []db.Insight
	}

	func initialModel(d *db.DB) *model {
	now := time.Now()
	// Normalize to midnight for consistent comparisons
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	ti := textinput.New()
	ti.Focus()

	m := &model{
		db:                d,
		metrics:           []metricDefinition{
	// ... (rest of metrics definition)

			{
				id:      "bp",
				label:   "Blood Pressure",
				tooltip: "Guided entry: Systolic, Diastolic, then Pulse",
				mType:   typeInput,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val > 0 && val < 300
				},
			},
			{
				id:      "alcohol",
				label:   "Alcohol Intake",
				tooltip: "Did you drink alcohol today?",
				mType:   typeToggle,
			},
			{
				id:      "hydration",
				label:   "Hydration",
				tooltip: "Target is 'Normal' hydration",
				mType:   typeEnum,
				options: []string{"Low", "Normal"},
			},
			{
				id:      "sleep",
				label:   "Sleep Duration",
				tooltip: "Guided entry: Hours then Minutes",
				mType:   typeInput,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val >= 0 && val < 60
				},
			},
			{
				id:      "training",
				label:   "Training",
				tooltip: "Walk > 30 min OR high pulse training > 30 min",
				mType:   typeToggle,
			},
			{
				id:      "stress",
				label:   "Stress Level",
				tooltip: "Perceived stress (1-5, 1=lowest)",
				mType:   typeRating,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val >= 1 && val <= 5
				},
			},
			{
				id:      "feel",
				label:   "Overall Feel",
				tooltip: "Perceived wellbeing (1-5, 1=lowest)",
				mType:   typeRating,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val >= 1 && val <= 5
				},
			},
		},
		values:            make(map[string]string),
		currentDate:       today,
		input:             ti,
		analyticsInterval: 7,
	}
	m.loadData()
	return m
	}

	func (m *model) loadAnalytics() {
	data, err := m.db.GetMetricDataInRange(m.analyticsInterval)
	if err != nil {
		m.err = err
	} else {
		m.analyticsData = data
	}

	insights, err := m.db.GetInsights(m.analyticsInterval)
	if err != nil {
		m.err = err
	} else {
		m.analyticsInsights = insights
	}
	}


func (m *model) loadData() {
	dateStr := m.currentDate.Format("2006-01-02")
	m.values = make(map[string]string)

	query := `SELECT metric_type, value FROM metrics WHERE date = ?`
	rows, err := m.db.Conn.Query(query, dateStr)
	if err != nil {
		m.err = err
		return
	}
	defer rows.Close()

	for rows.Next() {
		var mType, val string
		if err := rows.Scan(&mType, &val); err == nil {
			m.values[mType] = val
		}
	}

	// Fetch streak
	s, err := m.db.GetStreak()
	if err != nil {
		m.err = err
	} else {
		m.streak = s
	}
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.isInputting {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				val := m.input.Value()
				metric := m.metrics[m.cursor]
				if metric.validate != nil && !metric.validate(val) {
					return m, nil
				}

				m.tempValues = append(m.tempValues, val)
				m.input.Reset()

				// Check if more steps are needed
				dateStr := m.currentDate.Format("2006-01-02")
				if metric.id == "bp" && len(m.tempValues) < 3 {
					m.inputStep++
					return m, nil
				} else if metric.id == "sleep" && len(m.tempValues) < 2 {
					m.inputStep++
					return m, nil
				}

				// Finalize input
				finalVal := val
				if metric.id == "bp" {
					finalVal = fmt.Sprintf("%s/%s - %s", m.tempValues[0], m.tempValues[1], m.tempValues[2])
				} else if metric.id == "sleep" {
					h := m.tempValues[0]
					if len(h) == 1 {
						h = "0" + h
					}
					min := m.tempValues[1]
					if len(min) == 1 {
						min = "0" + min
					}
					finalVal = fmt.Sprintf("%s:%s", h, min)
				}

				m.isInputting = false
				m.inputStep = 0
				m.tempValues = nil
				m.values[metric.id] = finalVal
				m.db.DeleteMetric(metric.id, dateStr)
				m.db.LogMetric(metric.id, finalVal, dateStr)
				m.loadData() // Refresh streak and values
				return m, nil
			case "esc":
				m.isInputting = false
				m.inputStep = 0
				m.tempValues = nil
				m.input.Reset()
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.mode == modeDatabase || m.mode == modeAnalytics {
				m.mode = modeTracker
				return m, nil
			}
			return m, tea.Quit
		case "a":
			if m.mode == modeTracker {
				m.mode = modeAnalytics
				m.loadAnalytics()
			} else {
				m.mode = modeTracker
			}
		case "1":
			if m.mode == modeAnalytics {
				m.analyticsInterval = 7
				m.loadAnalytics()
			}
		case "2":
			if m.mode == modeAnalytics {
				m.analyticsInterval = 30
				m.loadAnalytics()
			}
		case "3":
			if m.mode == modeAnalytics {
				m.analyticsInterval = 90
				m.loadAnalytics()
			}
		case "d":
			if m.mode == modeTracker {
				m.mode = modeDatabase
				stats, err := m.db.GetStats()
				if err != nil {
					m.err = err
				} else {
					m.dbStats = stats
				}
			} else {
				m.mode = modeTracker
			}
		case "r":
			if m.mode == modeDatabase {
				if err := m.db.Reset(); err != nil {
					m.err = err
				} else {
					m.loadData()
					stats, _ := m.db.GetStats()
					m.dbStats = stats
				}
			}
		case "t":
			// Toggle Test Mode (In-Mem)
			if m.db.IsEphemeral {
				// Exit test mode (go back to real DB - simple reload)
				d, err := db.Open()
				if err != nil {
					m.err = err
				} else {
					m.db.Close()
					m.db = d
					m.loadData()
				}
			} else {
				// Enter test mode
				d, err := db.OpenInMem()
				if err != nil {
					m.err = err
				} else {
					d.SeedDummyData()
					m.db = d
					m.loadData()
				}
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.metrics)-1 {
				m.cursor++
			}
		case "left", "h":
			m.currentDate = m.currentDate.AddDate(0, 0, -1)
			m.loadData()
		case "right", "l":
			now := time.Now()
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			if m.currentDate.Before(today) {
				m.currentDate = m.currentDate.AddDate(0, 0, 1)
				m.loadData()
			}
		case "enter", " ":
			metric := m.metrics[m.cursor]
			dateStr := m.currentDate.Format("2006-01-02")
			switch metric.mType {
			case typeToggle:
				val := m.values[metric.id]
				m.db.DeleteMetric(metric.id, dateStr)
				if val == "true" {
					m.values[metric.id] = "false"
				} else {
					m.values[metric.id] = "true"
					m.db.LogMetric(metric.id, "true", dateStr)
				}
				m.loadData()
			case typeInput, typeRating:
				m.isInputting = true
				m.input.Placeholder = metric.placeholder
				if metric.mType == typeRating {
					m.input.Placeholder = "1-5"
				}
				return m, textinput.Blink
			case typeEnum:
				val := m.values[metric.id]
				nextIndex := 0
				for i, opt := range metric.options {
					if opt == val {
						nextIndex = (i + 1) % len(metric.options)
						break
					}
				}
				next := metric.options[nextIndex]
				m.values[metric.id] = next
				m.db.DeleteMetric(metric.id, dateStr)
				m.db.LogMetric(metric.id, next, dateStr)
				m.loadData()
			}
		}
	}
	return m, nil
}

func (m *model) View() string {
	ascii := ` ______     __     ______     __    __     ______     ______   ______     __  __    
/\  == \   /\ \   /\  __ \   /\ "-./  \   /\  ___\   /\__  _\ /\  == \   /\ \/ /    
\ \  __<   \ \ \  \ \ \/\ \  \ \ \-./\ \  \ \  __\   \/_/\ \/ \ \  __<   \ \  _"-.  
 \ \_____\  \ \_\  \ \_____\  \ \_\ \ \_\  \ \_____\    \ \_\  \ \_\ \_\  \ \_\ \_\ 
  \/_____/   \/_/   \/_____/   \/_/  \/_/   \/_____/     \/_/   \/_/ /_/   \/_/\/_/`
	
	s := ascii + "\n\n"
	
	if m.mode == modeDatabase {
		s += "Database Management\n\n"
		if m.dbStats == nil {
			s += "Loading stats...\n"
		} else {
			s += fmt.Sprintf("Total Entries:  %d\n", m.dbStats.TotalEntries)
			if m.dbStats.TotalEntries > 0 {
				s += fmt.Sprintf("First Entry:    %s\n", m.dbStats.FirstEntry)
				s += fmt.Sprintf("Last Entry:     %s\n", m.dbStats.LastEntry)
				s += fmt.Sprintf("Longest Streak: %d days 🏆\n", m.dbStats.LongestStreak)
				s += "\nBreakdown:\n"
				for mType, count := range m.dbStats.MetricCounts {
					s += fmt.Sprintf(" - %-15s: %d\n", mType, count)
				}
			}
		}
		s += "\nPress 'r' to RESET (DELETE ALL DATA). Press 'd' or 'q' to return.\n"
		return s
	}

	if m.mode == modeAnalytics {
		s += fmt.Sprintf("Analytics - Last %d Days\n\n", m.analyticsInterval)
		s += "Showing trends for each metric:\n\n"

		for _, metric := range m.metrics {
			data := m.analyticsData[metric.id]
			if len(data) < 2 {
				// We need at least 2 points to plot.
				// If we have 1 or 0, we can still plot something basic or show a message.
				if len(data) == 1 {
					data = append(data, data[0]) // Duplicate for plot
				} else {
					data = []float64{0, 0}
				}
			}

			s += fmt.Sprintf("%s:\n", metric.label)
			graph := asciigraph.Plot(data, 
				asciigraph.Height(5), 
				asciigraph.Width(50), 
				asciigraph.Precision(1))
			s += graph + "\n\n"
		}

		if len(m.analyticsInsights) > 0 {
			s += "Insights & Correlations:\n"
			for _, insight := range m.analyticsInsights {
				s += fmt.Sprintf(" • %s\n", insight.Text)
			}
			s += "\n"
		} else {
			s += "Insights: Keep logging data to see lifestyle correlations!\n\n"
		}

		s += fmt.Sprintf("\nIntervals: [1] 7 Days  [2] 30 Days  [3] 90 Days\n")
		s += "Press 'a' or 'q' to return to tracker.\n"
		return s
	}

	title := "Biometrk - Health Tracker"
	if m.db.IsEphemeral {
		title += " [TEST MODE]"
	}
	s += title + "\n\n"

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dateStr := m.currentDate.Format("2006-01-02")
	if m.currentDate.Equal(today) {
		dateStr += " (Today)"
	}
	s += fmt.Sprintf("Date: %-20s Streak: %d days, keep going! 🔥\n", dateStr, m.streak)
	s += "Use Left/Right to navigate days.\n\n"

	for i, metric := range m.metrics {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		
		val := m.values[metric.id]
		displayVal := "[ ]"
		if val == "true" {
			displayVal = "[x]"
		} else if val != "" && val != "false" {
			displayVal = fmt.Sprintf("[%s]", val)
		} else if metric.mType != typeToggle {
			displayVal = "[ ]"
		}

		s += fmt.Sprintf("%s %-20s %s\n", cursor, metric.label, displayVal)
	}

	activeMetric := m.metrics[m.cursor]
	s += fmt.Sprintf("\nTooltip: %s\n", activeMetric.tooltip)

	if m.isInputting {
		prompt := activeMetric.label
		if activeMetric.id == "bp" {
			steps := []string{"Systolic", "Diastolic", "Pulse"}
			prompt = steps[m.inputStep]
		} else if activeMetric.id == "sleep" {
			steps := []string{"Hours", "Minutes"}
			prompt = steps[m.inputStep]
		}
		s += fmt.Sprintf("\nEnter %s: %s\n", prompt, m.input.View())
		s += "(press Esc to cancel)\n"
	} else {
		s += "\nPress Enter to toggle/edit. Press 't' for Test Mode. Press 'd' for Stats. Press 'a' for Analytics. Press q to quit.\n"
	}

	if m.err != nil {
		s += fmt.Sprintf("\nError: %v\n", m.err)
	}

	s += "\n---\n"
	s += "Disclaimer: For personal tracking only. Not medical advice.\n"
	s += "Read more: https://github.com/bjornramberg/biometrk/\n"

	return s
}

func main() {
	d, err := db.Open()
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer d.Close()

	p := tea.NewProgram(initialModel(d))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
