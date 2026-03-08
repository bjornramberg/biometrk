package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bjornramberg/biometrk/internal/db"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	guidance    string
	source      string
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
	modeInsights
)

type model struct {
	db                *db.DB
	metrics           []metricDefinition
	values            map[string]string // metricID -> value
	cursor            int
	currentDate       time.Time
	err               error
	exportMsg         string
	input             textinput.Model
	isInputting       bool
	inputStep         int
	tempValues        []string
	mode              viewMode
	dbStats           *db.DBStats
	backups           []string
	streak            int
	analyticsInterval int // 7, 30, 90
	analyticsData     map[string][]float64
	analyticsInsights []db.Insight
	laggedInsights    []db.Insight
	width             int
	height            int
}

func initialModel(d *db.DB) *model {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	ti := textinput.New()
	ti.Focus()

	m := &model{
		db: d,
		metrics: []metricDefinition{
			{
				id:       "bp",
				label:    "Blood Pressure",
				tooltip:  "Guided entry: Systolic, Diastolic, then Pulse",
				guidance: "AHA Categories:\n • Normal: <120 / <80 mmHg\n • Elevated: 120-129 / <80 mmHg\n • Stage 1 Hypertension: 130-139 / 80-89 mmHg",
				source:   "American Heart Association (AHA)",
				mType:    typeInput,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val > 0 && val < 300
				},
			},

			{
				id:       "alcohol",
				label:    "Alcohol Intake",
				tooltip:  "Did you drink alcohol today?",
				guidance: "Moderate intake is defined as:\n • Women: Up to 1 drink/day\n • Men: Up to 2 drinks/day\nRisk of health issues increases with any consumption.",
				source:   "WHO / Dietary Guidelines for Americans",
				mType:    typeToggle,
			},
			{
				id:       "hydration",
				label:    "Hydration",
				tooltip:  "Target is 'Normal' hydration",
				guidance: "Aim for 'Normal' (roughly 2-3 liters of total fluids for most adults).",
				source:   "Mayo Clinic",
				mType:    typeEnum,
				options:  []string{"Low", "Normal"},
			},
			{
				id:       "sleep",
				label:    "Sleep Duration",
				tooltip:  "Guided entry: Hours then Minutes",
				guidance: "Most adults should aim for 7–9 hours of quality sleep per night.",
				source:   "National Sleep Foundation",
				mType:    typeInput,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val >= 0 && val < 60
				},
			},
			{
				id:       "training",
				label:    "Training",
				tooltip:  "Walk > 30 min OR high pulse training > 30 min",
				guidance: "Aim for at least 150 min of moderate activity per week.",
				source:   "WHO / CDC",
				mType:    typeToggle,
			},
			{
				id:       "stress",
				label:    "Stress Level",
				tooltip:  "Perceived stress (1-5, 1=lowest)",
				guidance: "Chronic high stress impacts both mental and physical health.",
				source:   "Mental Health America",
				mType:    typeRating,
				validate: func(s string) bool {
					val, err := strconv.Atoi(s)
					return err == nil && val >= 1 && val <= 5
				},
			},
			{
				id:       "feel",
				label:    "Overall Feel",
				tooltip:  "Perceived wellbeing (1-5, 1=lowest)",
				guidance: "Tracking subjective feel can help identify long-term wellness trends.",
				source:   "Biometrk Wellness Tracking",
				mType:    typeRating,
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

	lags, err := m.db.GetLeadLagInsights(m.analyticsInterval)
	if err != nil {
		m.err = err
	} else {
		m.laggedInsights = lags
	}
}

func (m *model) loadBackups() {
	files, err := os.ReadDir("backups")
	if err != nil {
		return
	}
	m.backups = nil
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".db") {
			m.backups = append(m.backups, filepath.Join("backups", f.Name()))
		}
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

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
			if m.mode == modeDatabase || m.mode == modeAnalytics || m.mode == modeInsights {
				m.mode = modeTracker
				m.exportMsg = ""
				return m, nil
			}
			return m, tea.Quit
		case "a":
			if m.mode == modeTracker {
				m.mode = modeAnalytics
				m.exportMsg = ""
				m.loadAnalytics()
			} else {
				m.mode = modeTracker
			}
		case "i":
			if m.mode == modeTracker {
				m.mode = modeInsights
				m.exportMsg = ""
				m.loadAnalytics()
			} else {
				m.mode = modeTracker
			}
		case "1":
			if m.mode == modeAnalytics || m.mode == modeInsights {
				m.analyticsInterval = 7
				m.loadAnalytics()
			}
		case "2":
			if m.mode == modeAnalytics || m.mode == modeInsights {
				m.analyticsInterval = 30
				m.loadAnalytics()
			}
		case "3":
			if m.mode == modeAnalytics || m.mode == modeInsights {
				m.analyticsInterval = 90
				m.loadAnalytics()
			}
		case "d":
			if m.mode == modeTracker {
				m.mode = modeDatabase
				m.loadBackups()
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
		case "b":
			if m.mode == modeDatabase {
				_, err := m.db.Backup()
				if err != nil {
					m.err = err
				} else {
					m.loadBackups()
					stats, _ := m.db.GetStats()
					m.dbStats = stats
				}
			}
		case "e":
			if m.mode == modeDatabase {
				path, err := m.db.ExportCSV()
				if err != nil {
					m.err = err
				} else {
					m.exportMsg = fmt.Sprintf("Exported to %s", path)
				}
			}
		case "m":
			if m.mode == modeDatabase {
				path, err := m.db.ExportMarkdown()
				if err != nil {
					m.err = err
				} else {
					m.exportMsg = fmt.Sprintf("Report saved to %s", path)
				}
			}
		case "R":
			if m.mode == modeDatabase && len(m.backups) > 0 {
				latest := m.backups[len(m.backups)-1]
				if err := m.db.Restore(latest); err != nil {
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

func getMetricColor(id, value string) lipgloss.Color {
	if value == "" || value == "false" {
		return lipgloss.Color("252") // Default gray
	}

	switch id {
	case "bp":
		// Format: systolic/diastolic - pulse
		parts := strings.Split(value, "/")
		if len(parts) < 2 {
			return lipgloss.Color("252")
		}
		sys, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		diaParts := strings.Split(parts[1], "-")
		dia, _ := strconv.Atoi(strings.TrimSpace(diaParts[0]))

		if sys < 120 && dia < 80 {
			return lipgloss.Color("42") // Green
		}
		if sys < 130 && dia < 80 {
			return lipgloss.Color("220") // Yellow
		}
		return lipgloss.Color("196") // Red

	case "sleep":
		// Format: HH:MM
		parts := strings.Split(value, ":")
		if len(parts) < 2 {
			return lipgloss.Color("252")
		}
		h, _ := strconv.Atoi(parts[0])
		if h >= 7 && h <= 9 {
			return lipgloss.Color("42") // Green
		}
		if h == 6 || h == 10 {
			return lipgloss.Color("220") // Yellow
		}
		return lipgloss.Color("196") // Red

	case "stress":
		val, _ := strconv.Atoi(value)
		if val <= 2 {
			return lipgloss.Color("42") // Green
		}
		if val == 3 {
			return lipgloss.Color("220") // Yellow
		}
		return lipgloss.Color("196") // Red

	case "feel":
		val, _ := strconv.Atoi(value)
		if val >= 4 {
			return lipgloss.Color("42") // Green
		}
		if val == 3 {
			return lipgloss.Color("220") // Yellow
		}
		return lipgloss.Color("196") // Red

	case "alcohol":
		if value == "true" {
			return lipgloss.Color("208") // Orange/Warning
		}
		return lipgloss.Color("42") // Green (No alcohol)

	case "hydration", "training":
		if value == "true" || value == "Normal" {
			return lipgloss.Color("42") // Green
		}
		return lipgloss.Color("220") // Yellow
	}

	return lipgloss.Color("252")
}

func (m *model) View() string {
	// 1. MASTER DIMENSIONS
	totalWidth := m.width - 6
	if totalWidth < 115 { totalWidth = 115 }
	
	// Available height for the main box, excluding header (8), menu (2), and footer (3)
	totalHeight := m.height - 13
	if totalHeight < 10 { totalHeight = 10 }

	// 2. STYLES
	var (
		headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
		dateStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).MarginLeft(2)
		streakStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				MarginLeft(2)

		metricLabelStyle = lipgloss.NewStyle(). Width(25). Foreground(lipgloss.Color("252"))
		metricValueStyle = lipgloss.NewStyle()

		// mainBorderStyle (No Width/Height set here to prevent breaking)
		mainBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(1)
		
		infoBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(0, 1)
	)

	// Apply dynamic streak style
	if m.streak >= 7 {
		streakStyle = streakStyle.Foreground(lipgloss.Color("208"))
	}
	if m.streak >= 30 {
		streakStyle = streakStyle.Foreground(lipgloss.Color("196")).Bold(true)
	}

	// 3. HEADER ASSEMBLY
	ascii := ` ______     __     ______     __    __     ______     ______   ______     __  __    
/\  == \   /\ \   /\  __ \   /\ "-./  \   /\  ___\   /\__  _\ /\  == \   /\ \/ /    
\ \  __<   \ \ \  \ \ \/\ \  \ \ \-./\ \  \ \  __\   \/_/\ \/ \ \  __<   \ \  _"-.  
 \ \_____\  \ \_\  \ \_____\  \ \_\ \ \_\  \ \_____\    \ \_\  \ \_\ \_\  \ \_\ \_\ 
  \/_____/   \/_/   \/_____/   \/_/  \/_/   \/_____/     \/_/   \/_/ /_/   \/_/\/_/`
	ascii = strings.TrimPrefix(ascii, "\n")
	logo := headerStyle.Render(ascii)
	logoWidth := lipgloss.Width(logo)

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dateStr := m.currentDate.Format("2006-01-02")
	if m.currentDate.Equal(today) { dateStr += " (Today)" }

	// Info box width must exactly fill the remaining space to hit totalWidth
	// TotalWidth - LogoWidth - 2 (spacer)
	infoBoxOuterWidth := totalWidth - logoWidth - 2
	
	infoContent := ""
	if m.db.IsEphemeral {
		infoContent += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("[TEST MODE]") + "\n"
	}
	infoContent += fmt.Sprintf("Date: %s\n", dateStyle.Render(dateStr))
	infoContent += fmt.Sprintf("Streak: %s\n", streakStyle.Render(fmt.Sprintf("%d days 🔥", m.streak)))
	infoContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("Navigate: ← / → keys")

	// Apply Width to the content inside the info box border
	placedInfo := lipgloss.Place(infoBoxOuterWidth-2, 4, lipgloss.Left, lipgloss.Top, infoContent)
	infoBox := infoBorderStyle.Render(placedInfo)

	header := lipgloss.JoinHorizontal(lipgloss.Top, logo, "  ", infoBox) + "\n\n"

	// 4. MAIN CONTENT ASSEMBLY
	var content string
	switch m.mode {
	case modeDatabase:
		content = "Database Management\n\n"
		if m.dbStats == nil {
			content += "Loading stats...\n"
		} else {
			sizeStr := fmt.Sprintf("%.2f KB", float64(m.dbStats.Size)/1024)
			if m.dbStats.Size > 1024*1024 { sizeStr = fmt.Sprintf("%.2f MB", float64(m.dbStats.Size)/(1024*1024)) }
			content += fmt.Sprintf("Location:       %s\n", m.dbStats.Path)
			content += fmt.Sprintf("File Size:      %s\n", sizeStr)
			content += fmt.Sprintf("Total Entries:  %d\n", m.dbStats.TotalEntries)
			if m.dbStats.TotalEntries > 0 {
				content += fmt.Sprintf("Date Range:     %s to %s\n", m.dbStats.FirstEntry, m.dbStats.LastEntry)
				content += fmt.Sprintf("Longest Streak: %d days 🏆\n", m.dbStats.LongestStreak)
			}
			if len(m.backups) > 0 {
				content += "\nAvailable Backups:\n"
				for i, b := range m.backups {
					if i >= 5 { content += " ...\n"; break }
					content += fmt.Sprintf(" • %s\n", filepath.Base(b))
				}
			}

			if m.exportMsg != "" {
				content += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Render("✔ "+m.exportMsg) + "\n"
			}
		}
		content += "\nPress 'b' to BACKUP. Press 'R' to RESTORE. Press 'e' to CSV. Press 'm' to MARKDOWN. Press 'r' to RESET. Press 'd' or 'q' to return."

	case modeAnalytics:
		content = fmt.Sprintf("Analytics - Last %d Days\n\n", m.analyticsInterval)
		chartWidth := (totalWidth - 20) / 2
		if chartWidth < 20 { chartWidth = 20 }
		if chartWidth > 60 { chartWidth = 60 }

		var graphs []string
		for _, metric := range m.metrics {
			data := m.analyticsData[metric.id]
			if len(data) < 2 { data = []float64{0, 0} }
			graphContent := fmt.Sprintf("%s:\n", metric.label)
			g := asciigraph.Plot(data, asciigraph.Height(5), asciigraph.Width(chartWidth), asciigraph.Precision(1))
			graphContent += g
			
			startD := time.Now().AddDate(0, 0, -m.analyticsInterval+1).Format("01-02")
			midD := time.Now().AddDate(0, 0, -(m.analyticsInterval / 2)).Format("01-02")
			endD := time.Now().Format("01-02")
			padding := (chartWidth + 7 - len(startD) - len(midD) - len(endD)) / 2
			if padding < 1 { padding = 1 }
			axis := "\n" + startD + strings.Repeat(" ", padding) + midD + strings.Repeat(" ", padding) + endD
			graphContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(axis)
			graphs = append(graphs, lipgloss.NewStyle().Width(chartWidth+7).Render(graphContent))
		}

		if totalWidth > 100 {
			var rows []string
			for i := 0; i < len(graphs); i += 2 {
				if i+1 < len(graphs) { rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, graphs[i], "    ", graphs[i+1])) } else { rows = append(rows, graphs[i]) }
			}
			content += strings.Join(rows, "\n\n")
		} else { content += strings.Join(graphs, "\n\n") }

	case modeInsights:
		content = fmt.Sprintf("Lifestyle Insights - Last %d Days\n\n", m.analyticsInterval)
		content += "Direct Correlations:\n"
		if len(m.analyticsInsights) > 0 {
			for _, insight := range m.analyticsInsights { content += fmt.Sprintf(" • %s\n", insight.Text) }
		} else { content += " No strong correlations found yet.\n" }
		content += "\nLead/Lag (Yesterday vs Today):\n"
		if len(m.laggedInsights) > 0 {
			for _, insight := range m.laggedInsights { content += fmt.Sprintf(" • %s\n", insight.Text) }
		} else { content += " No significant patterns detected.\n" }

	default: // modeTracker
		activeMetric := m.metrics[m.cursor]
		listContent := ""
		for i, metric := range m.metrics {
			cursor := "  "; if m.cursor == i { cursor = "> " }
			val := m.values[metric.id]
			displayVal := "[ ]"
			if val == "true" { 
				displayVal = "[x]" 
			} else if val != "" && val != "false" { 
				displayVal = fmt.Sprintf("[%s]", val) 
			}
			
			// Dynamic Color
			color := getMetricColor(metric.id, val)
			styledVal := metricValueStyle.Foreground(color).Render(displayVal)
			
			listContent += fmt.Sprintf("%s%s %s\n", cursor, metricLabelStyle.Render(metric.label), styledVal)
		}
		if m.isInputting {
			prompt := activeMetric.label
			if activeMetric.id == "bp" { prompt = []string{"Systolic", "Diastolic", "Pulse"}[m.inputStep] } else if activeMetric.id == "sleep" { prompt = []string{"Hours", "Minutes"}[m.inputStep] }
			listContent += fmt.Sprintf("\nEnter %s: %s\n", prompt, m.input.View())
			listContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(press Esc to cancel)")
		}
		divider := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("-", totalWidth/2-6))
		tipContent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render("Metric Details") + "\n"
		tipContent += lipgloss.NewStyle().Bold(true).Render(activeMetric.label) + "\n"
		tipContent += divider + "\n\n"
		tipContent += lipgloss.NewStyle().Bold(true).Render("How to Enter:") + "\n"
		tipContent += activeMetric.tooltip + "\n\n"
		if activeMetric.guidance != "" {
			tipContent += lipgloss.NewStyle().Bold(true).Render("Health Guidance:") + "\n"
			tipContent += activeMetric.guidance + "\n\n"
		}
		if activeMetric.source != "" {
			tipContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Source: "+activeMetric.source)
		}
		
		content = lipgloss.JoinHorizontal(lipgloss.Top, 
			lipgloss.NewStyle().Width(totalWidth/2).Render(listContent),
			lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(1).Width(totalWidth/2-4).Render(tipContent))
	}

	// 5. FINAL ASSEMBLY
	menuStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1).MarginTop(1)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	menuItems := []string{
		keyStyle.Render("enter") + " edit", keyStyle.Render("t") + " test mode",
		keyStyle.Render("d") + " database", keyStyle.Render("a") + " analytics",
		keyStyle.Render("i") + " insights", keyStyle.Render("q") + " quit",
	}
	if m.mode == modeAnalytics || m.mode == modeInsights { menuItems = append(menuItems, keyStyle.Render("1-3") + " interval") }
	if m.mode == modeDatabase { menuItems = append(menuItems, keyStyle.Render("b")+" backup", keyStyle.Render("R")+" restore", keyStyle.Render("e")+" csv", keyStyle.Render("m")+" md", keyStyle.Render("r")+" reset") }
	menuBar := menuStyle.Render(strings.Join(menuItems, "  •  "))

	disclaimer := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Disclaimer: For personal tracking only. Not medical advice. Read more: https://github.com/bjornramberg/biometrk/")

	// Size the content block perfectly to fit INSIDE the borders
	placedMain := lipgloss.Place(totalWidth-2, totalHeight-2, lipgloss.Left, lipgloss.Top, content)
	mainBox := mainBorderStyle.Render(placedMain)

	return header + mainBox + "\n" + menuBar + "\n" + disclaimer
}

func main() {
	d, err := db.Open()
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer d.Close()

	p := tea.NewProgram(initialModel(d), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
