package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/bjornramberg/biometrk/internal/db"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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

type model struct {
	db          *db.DB
	metrics     []metricDefinition
	values      map[string]string // metricID -> value
	cursor      int
	currentDate time.Time
	err         error
	input       textinput.Model
	isInputting bool
	inputStep   int
	tempValues  []string
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
		values:      make(map[string]string),
		currentDate: today,
		input:       ti,
	}
	m.loadData()
	return m
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
			return m, tea.Quit
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
			}
		}
	}
	return m, nil
}

func (m *model) View() string {
	s := "Biometrk - Health Tracker\n\n"

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dateStr := m.currentDate.Format("2006-01-02")
	if m.currentDate.Equal(today) {
		dateStr += " (Today)"
	}
	s += fmt.Sprintf("Date: %s\n", dateStr)
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
		s += "\nPress Enter to toggle/edit. Press q to quit.\n"
	}

	if m.err != nil {
		s += fmt.Sprintf("\nError: %v\n", m.err)
	}
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
