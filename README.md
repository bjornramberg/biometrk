# Biometrk

A privacy-focused health tracker/checker app for the Linux terminal.

## Features

- **TUI (Terminal User Interface):** Simple, fast, and keyboard-driven.
- **Privacy First:** All data is stored locally in a SQLite database. No personal data is sent to the cloud.
- **Tracked Metrics:**
  - Blood Pressure
  - Alcohol Intake
  - Hydration
  - Sleep
  - Training
  - Stress
  - Overall Feel
- **Reporting & Analysis:** Gain insights from your data over time.

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

*Biometrk is for personal health tracking and analysis purposes only. It is NOT intended to provide medical advice, diagnosis, or treatment, and it should NOT replace professional consultations with a physician or healthcare provider. All use and interpretation of data are at the user's own responsibility.*

## License

MIT
