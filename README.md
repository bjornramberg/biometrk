# Biometrk

A privacy-focused health tracker/checker app for the Linux terminal.

## Features

- **Modern TUI Dashboard:** A structured, responsive Terminal User Interface built with Bubble Tea and Lip Gloss.
- **Privacy First:** All data is stored locally in a SQLite database. No personal data is sent to the cloud.
- **Guided Data Entry:** Multi-step input for complex metrics like Blood Pressure and Sleep.
- **Health Insights:**
  - **Analytics:** Visualize trends with ASCII line graphs for 7, 30, and 90-day intervals.
  - **Lifestyle Correlations:** Discover how your habits impact your wellbeing.
  - **Lead/Lag Analysis:** Identify if yesterday's choices (like sleep or training) affect today's outcomes.
- **Expert Context:** Integrated health benchmarks and guidance from authoritative sources:
  - American Heart Association (AHA)
  - National Sleep Foundation
  - WHO / Dietary Guidelines for Americans
  - Mayo Clinic
- **Gamification:** Track your current and historical longest logging streaks.
- **Database Management:**
  - **Stats:** View file size, location, and entry counts.
  - **Backups:** Create timestamped safety copies of your records.
  - **Recovery:** Restore from previous backups directly within the app.
  - **Portability:** Export your entire history to CSV or Markdown reports.
- **Test Mode:** Toggle an ephemeral in-memory environment with 3 weeks of dummy data for exploration.

## Getting Started

### Prerequisites

- Go (1.21 or later)

### Installation

Clone the repository:

```bash
git clone https://github.com/bjornramberg/biometrk.git
cd biometrk
```

### Build and Run

To run the application directly:

```bash
go run ./cmd/biometrk
```

To build the binary:

```bash
go build -o biometrk ./cmd/biometrk
./biometrk
```

## Disclaimer

**Biometrk is for personal health tracking and analysis purposes only. It is NOT intended to provide medical advice, diagnosis, or treatment, and it should NOT replace professional consultations with a physician or healthcare provider. All use and interpretation of data are at the user's own responsibility.**

## License

MIT
