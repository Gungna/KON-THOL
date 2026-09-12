# KON-THOL Engine (Go Edition)

Engine otomasi dan notifikasi portal ETHOL PENS berbasis bahasa pemrograman Go.

## Keunggulan Arsitektur
- Footprint memori sangat rendah (~8 MB RSS) dibandingkan runtime Python (~60 MB).
- Single native binary tanpa ketergantungan runtime eksternal.
- Manajemen koneksi HTTP pooling dengan keep-alive dan cookie jar otomatis.
- Scheduler adaptif multi-tier (Siaga Subuh, Siaga Penuh Kuliah, Istirahat Malam).
- Telegram bot single-view UI card dengan in-place message editing.

## Struktur Direktori
```
konthol-go/
├── client/
│   └── client.go       # HTTP client, CAS SSO login, API caller
├── config/
│   └── config.go       # Parser credentials & multi-account configuration
├── telegram/
│   └── bot.go          # Telegram bot handler & UI keyboard layout
├── go.mod              # Definisi modul Go
└── main.go             # Entry point daemon dan scheduler loop
```

## Kompilasi & Menjalankan

### Persyaratan
- Go versi 1.22 atau lebih baru

### Build Binary
```bash
go mod tidy
go build -o konthol main.go
```

### Menjalankan Service
```bash
./konthol -cred credentials.json -state attended_keys.json
```
