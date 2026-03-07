package main

import (
	"fmt"
	"os"
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
	err         error
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
	laggedInsights    []db.Insight
	width             int
	height            int
}

func initialModel(d *db.DB) *model {
	now := time.Now()
	// Normalize to midnight for consistent comparisons
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	ti := textinput.New()
	ti.Focus()

	m := &model{
		db: d,
		metrics: []metricDefinition{
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

	lags, err := m.db.GetLeadLagInsights(m.analyticsInterval)
	if err != nil {
		m.err = err
	} else {
		m.laggedInsights = lags
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
		case "i":
			if m.mode == modeTracker {
				m.mode = modeInsights
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
	// Styles
	var (
		headerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)
		dateStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				MarginLeft(2)
		streakStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				MarginLeft(2)
		metricLabelStyle = lipgloss.NewStyle().
					Width(25).
					Foreground(lipgloss.Color("252"))
		metricValueStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("212"))
		tooltipStyle = lipgloss.NewStyle().
				Italic(true).
				Foreground(lipgloss.Color("245")).
				MarginTop(1)
		
		// Enforce stable size for the main box
		boxWidth  = m.width - 4
		boxHeight = m.height - 12 // Reduced offset since header is more compact
		
		boxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(1).
				BorderForeground(lipgloss.Color("62")).
				Width(boxWidth).
				Height(boxHeight)
	)

	if boxWidth < 40 { boxWidth = 40 }
	if boxHeight < 10 { boxHeight = 10 }

	// 1. PERSISTENT HEADER (LOGO + INFO BOX)
	ascii := ` ______     __     ______     __    __     ______     ______   ______     __  __    
/\  == \   /\ \   /\  __ \   /\ "-./  \   /\  ___\   /\__  _\ /\  == \   /\ \/ /    
\ \  __<   \ \ \  \ \ \/\ \  \ \ \-./\ \  \ \  __\   \/_/\ \/ \ \  __<   \ \  _"-.  
 \ \_____\  \ \_\  \ \_____\  \ \_\ \ \_\  \ \_____\    \ \_\  \ \_\ \_\  \ \_\ \_\ 
  \/_____/   \/_/   \/_____/   \/_/  \/_/   \/_____/     \/_/   \/_/ /_/   \/_/\/_/`
	ascii = strings.TrimPrefix(ascii, "\n")

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dateStr := m.currentDate.Format("2006-01-02")
	if m.currentDate.Equal(today) {
		dateStr += " (Today)"
	}

	logo := headerStyle.Render(ascii)
	logoWidth := lipgloss.Width(logo)
	
	// Remaining width for the info box to hit the right edge
	infoBoxWidth := boxWidth - logoWidth - 2 
	if infoBoxWidth < 35 { infoBoxWidth = 35 }

	infoBoxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1).
			Width(infoBoxWidth)

	infoContent := ""
	if m.db.IsEphemeral {
		infoContent += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("[TEST MODE]") + "\n"
	}
	infoContent += fmt.Sprintf("Date: %s\n", dateStyle.Render(dateStr))
	infoContent += fmt.Sprintf("Streak: %s\n", streakStyle.Render(fmt.Sprintf("%d days 🔥", m.streak)))
	infoContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("Navigate: ← / → keys")

	infoBox := infoBoxStyle.Render(infoContent)

	header := lipgloss.JoinHorizontal(lipgloss.Top, logo, "  ", infoBox) + "\n\n"

	// 2. MAIN CONTENT (Boxed)
	var content string

	switch m.mode {
	case modeDatabase:
		content = "Database Management\n\n"
		if m.dbStats == nil {
			content += "Loading stats...\n"
		} else {
			sizeStr := fmt.Sprintf("%.2f KB", float64(m.dbStats.Size)/1024)
			if m.dbStats.Size > 1024*1024 {
				sizeStr = fmt.Sprintf("%.2f MB", float64(m.dbStats.Size)/(1024*1024))
			}

			content += fmt.Sprintf("Location:       %s\n", m.dbStats.Path)
			content += fmt.Sprintf("File Size:      %s\n", sizeStr)
			content += fmt.Sprintf("Total Entries:  %d\n", m.dbStats.TotalEntries)

			if m.dbStats.TotalEntries > 0 {
				content += fmt.Sprintf("Date Range:     %s to %s\n", m.dbStats.FirstEntry, m.dbStats.LastEntry)
				content += fmt.Sprintf("Longest Streak: %d days 🏆\n", m.dbStats.LongestStreak)
			}
		}
		content += "\nPress 'b' to BACKUP. Press 'r' to RESET. Press 'd' or 'q' to return."

	case modeAnalytics:
		content = fmt.Sprintf("Analytics - Last %d Days\n\n", m.analyticsInterval)
		chartWidth := (boxWidth - 20) / 2
		if chartWidth < 20 { chartWidth = 20 }
		if chartWidth > 60 { chartWidth = 60 }

		var graphs []string
		for _, metric := range m.metrics {
			data := m.analyticsData[metric.id]
			if len(data) < 2 {
				data = []float64{0, 0}
			}

			graphContent := fmt.Sprintf("%s:\n", metric.label)
			g := asciigraph.Plot(data, asciigraph.Height(5), asciigraph.Width(chartWidth), asciigraph.Precision(1))
			graphContent += g

			startD := time.Now().AddDate(0, 0, -m.analyticsInterval+1).Format("01-02")
			midD := time.Now().AddDate(0, 0, -(m.analyticsInterval / 2)).Format("01-02")
			endD := time.Now().Format("01-02")
			
			labelWidth := 7
			totalGraphArea := chartWidth + labelWidth
			padding := (totalGraphArea - len(startD) - len(midD) - len(endD)) / 2
			if padding < 1 { padding = 1 }
			
			axis := "\n" + startD + strings.Repeat(" ", padding) + midD + strings.Repeat(" ", padding) + endD
			graphContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(axis)
			graphs = append(graphs, lipgloss.NewStyle().Width(totalGraphArea).Render(graphContent))
		}

		if boxWidth > 100 {
			var rows []string
			for i := 0; i < len(graphs); i += 2 {
				if i+1 < len(graphs) {
					rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, graphs[i], "    ", graphs[i+1]))
				} else {
					rows = append(rows, graphs[i])
				}
			}
			content += strings.Join(rows, "\n\n")
		} else {
			content += strings.Join(graphs, "\n\n")
		}

	case modeInsights:
		content = fmt.Sprintf("Lifestyle Insights - Last %d Days\n\n", m.analyticsInterval)
		content += "Direct Correlations:\n"
		if len(m.analyticsInsights) > 0 {
			for _, insight := range m.analyticsInsights {
				content += fmt.Sprintf(" • %s\n", insight.Text)
			}
		} else { content += " No strong correlations found yet.\n" }

		content += "\nLead/Lag (Yesterday vs Today):\n"
		if len(m.laggedInsights) > 0 {
			for _, insight := range m.laggedInsights {
				content += fmt.Sprintf(" • %s\n", insight.Text)
			}
		} else { content += " No significant patterns detected.\n" }

	default: // modeTracker
		trackerContent := ""
		for i, metric := range m.metrics {
			cursor := "  "
			if m.cursor == i { cursor = "> " }
			val := m.values[metric.id]
			displayVal := "[ ]"
			if val == "true" { displayVal = "[x]" } else if val != "" && val != "false" { displayVal = fmt.Sprintf("[%s]", val) }
			trackerContent += fmt.Sprintf("%s%s %s\n", cursor, metricLabelStyle.Render(metric.label), metricValueStyle.Render(displayVal))
		}
		activeMetric := m.metrics[m.cursor]
		trackerContent += tooltipStyle.Render("Tooltip: "+activeMetric.tooltip) + "\n"

		if m.isInputting {
			prompt := activeMetric.label
			if activeMetric.id == "bp" {
				prompt = []string{"Systolic", "Diastolic", "Pulse"}[m.inputStep]
			} else if activeMetric.id == "sleep" {
				prompt = []string{"Hours", "Minutes"}[m.inputStep]
			}
			trackerContent += fmt.Sprintf("\nEnter %s: %s\n", prompt, m.input.View())
			trackerContent += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(press Esc to cancel)")
		}
		content = trackerContent
	}

	// 4. PERSISTENT MENU BAR
	menuStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236")).
			Padding(0, 1).
			MarginTop(1)
	
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	
	menuItems := []string{
		keyStyle.Render("enter") + " edit",
		keyStyle.Render("t") + " test mode",
		keyStyle.Render("d") + " stats",
		keyStyle.Render("a") + " analytics",
		keyStyle.Render("i") + " insights",
		keyStyle.Render("q") + " quit",
	}
	
	// Add context-specific keys
	if m.mode == modeAnalytics || m.mode == modeInsights {
		menuItems = append(menuItems, keyStyle.Render("1-3") + " interval")
	}
	if m.mode == modeDatabase {
		menuItems = append(menuItems, keyStyle.Render("b")+" backup")
		menuItems = append(menuItems, keyStyle.Render("r")+" reset")
	}

	menuBar := menuStyle.Render(strings.Join(menuItems, "  •  "))

	disclaimer := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("\nDisclaimer: For personal tracking only. Not medical advice. Read more: https://github.com/bjornramberg/biometrk/")

	// Assemble
	return header + boxStyle.Render(content) + menuBar + disclaimer
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
