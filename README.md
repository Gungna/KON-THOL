<p align="center">
  <img src="assets/banner_github.jpg" alt="KON-THOL Banner" width="100%">
</p>

# KON-THOL 👀🐦
### "Kawan Otomasi dan Notifikasi untuk E-THOL PENS"

**KON-THOL** adalah bot asisten automasi pribadi untuk kebutuhan perkuliahan yang cerdas berbasis Python & Telegram Bot untuk mahasiswa Politeknik TERBAEKKK se-Asia Tenggara. Dirancang untuk mempermudah mahasiswa memantau presensi, mengecek jadwal kuliah, serta mencatat tugas yang belum dikumpulkan dengan antarmuka yang sangat mudah digunakan.

---

## Pilihan Penggunaan: Di-pair ke Bot Telegram atau Dijalankan dari Terminal / HP

Bot **KON-THOL** dirancang paling optimal ketika **di-pair / diintegrasikan dengan Bot Telegram pribadi** Anda:
- Anda memiliki kendali langsung di genggaman smartphone tanpa perlu selalu membuka laptop atau terminal.
- Notifikasi presensi berhasil, tugas baru, dan pengingat deadline langsung masuk ke chat Telegram Anda.
- Seluruh perintah dapat diakses cepat menggunakan *slash command* standar maupun antarmuka tombol interaktif (*inline keyboard*).

> [!TIP]
> Menghubungkan bot ke Telegram pribadi adalah cara paling nyaman: seluruh informasi jadwal, rekap kehadiran, dan tugas kuliah bisa diakses cukup dengan satu ketukan tombol di HP Anda tanpa repot menyalakan laptop atau terminal.

**Apakah bisa dipakai tanpa Telegram (Hanya dari Terminal)?**  
Bisa. Anda tetap dapat menjalankan sistem di laptop atau HP (Termux) secara mandiri:
1. **Notifikasi Tetap Masuk Langsung ke Perangkat:**
   - **Di HP Android (Termux):** Notifikasi presensi dan tugas akan muncul sebagai banner notifikasi di status bar HP Anda (menggunakan paket `termux-api`).
   - **Di Desktop Windows:** Notifikasi presensi muncul otomatis sebagai Windows Toast Notification di sudut layar.
   - **Di Konsol Terminal (Linux):** Konfirmasi presensi dikirim ke notifikasi desktop (`notify-send`) serta dicetak rapi dan jelas secara real-time di layar terminal.
2. **Mode Interaktif Terminal (Ketik Perintah Langsung):**
   Saat skrip berjalan di terminal atau Termux, Anda bisa langsung mengetik perintah interaktif seperti `scan`, `jadwal`, `tugas`, `rekap`, `log`, `status`, `cooldown`, atau `resume` untuk mendapatkan respon langsung di layar terminal Anda.

> [!IMPORTANT]
> **Catatan Stabilitas:** Selain penggunaan melalui bot Telegram, **BELUM DAPAT DIPASTIKAN BAHWA YANG DIPAKAI AKAN STABLE**. Hal ini karena manajemen baterai background pada smartphone (Termux) maupun mode sleep pada laptop dapat mematikan proses sewaktu-waktu di luar kendali kita. Mode operasional yang **pasti stable dan teruji** adalah ketika di-pair dengan Bot Telegram pribadi Anda (terlebih jika dijalankan di VPS/server Linux 24/7).

---

## Fitur Utama

- **Otomasi Presensi Tenang & Cepat**:  
  Memantau dan mengisi presensi secara otomatis ketika sesi kuliah dibuka oleh dosen, sehingga Anda tidak perlu cemas terlewat sesi presensi saat sedang fokus menyimak materi.
- **Antarmuka Rapi & Otomatis (Single-View Clean UI)**:  
  Chat Telegram tetap bersih dan rapi layaknya dashboard aplikasi. Perintah yang telah usang beserta pesan interaksi perantara otomatis dibersihkan dari ruang chat, menyisakan Banner Menu utama di atas dan hasil perintah terbaru Anda di bawahnya.
- **Transisi Visual Progresif (Loading Bar 50%-100%)**:  
  Umpan balik seketika saat memindai presensi atau memuat data dengan indikator visual dinamis sehingga pengguna mengetahui status pemrosesan sistem secara transparan.
- **Jadwal Operasional Cerdas (Siaga Subuh & Istirahat Malam)**:  
  - 🌅 **Siaga Subuh (Mulai 04:00 WIB)**: Aktif sejak waktu sebelum azan Subuh berkumandang untuk siaga memantau persiapan sesi dan jadwal perkuliahan hari ini.  
  - 🟢 **Siaga Penuh (06:30 - 21:30 WIB)**: Memantau presensi dan jadwal secara aktif di jam perkuliahan reguler.  
  - 💤 **Istirahat Malam (21:30 - 04:00 WIB)**: Mengistirahatkan frekuensi polling saat larut malam karena tidak ada perkuliahan aktif di tengah malam, menghemat beban server secara etis dan efisien.
- **Jadwal Kuliah Rapi & Info Dosen**:  
  Menampilkan jadwal mingguan rapi terurut hari & jam lengkap dengan waktu, ruang kuliah, nama dosen pengampu, serta indikator kehadiran untuk mata kuliah hari ini.
- **Pemantau Tugas & Tautan Portal ETHOL**:  
  Menyajikan daftar tugas kuliah yang belum dikumpulkan secara rapi, lengkap dengan sisa waktu tenggat dan tautan langsung untuk membuka halaman pengumpulan tugas di ETHOL.
- **Statistik & Rekapitulasi Kehadiran Resmi**:  
  Melihat persentase kehadiran semester berjalan serta rincian sesi kehadiran per mata kuliah secara transparan dan akurat.
- **Filter Log Aktivitas 5 Kategori**:  
  Menyajikan riwayat log terpilah (Log Error, Log Login & Listener, Log Notif Masuk, Log Presensi Berhasil, dan Log Global) langsung dari sub-menu Telegram.
- **Edisi Khusus: Multi-Account Concurrency & WhatsApp Gateway (`kon_thol_public.py`)**:  
  Mendukung pemindaian dan pengisian presensi otomatis untuk 2 atau lebih akun mahasiswa sekaligus secara serentak (*concurrent multithreading*) menggunakan `ThreadPoolExecutor`, lengkap dengan notifikasi WhatsApp Gateway real-time (Fonnte, WPPConnect, atau Webhook) serta fallback Telegram.

---

## Panduan Rekomendasi Deployment: Mending Pakai Apa & di Mana?

Untuk mendapatkan efisiensi dan performa maksimal, berikut rekomendasi runtime berdasarkan perangkat yang digunakan:

```
┌─────────────────────────┬─────────────────────────┬────────────────────────────────────────────────────────┐
│ Lingkungan / Host       │ Pilihan Engine Terbaik  │ Alasan & Keunggulan                                    │
├─────────────────────────┼─────────────────────────┼────────────────────────────────────────────────────────┤
│ 📱 HP Android (Termux)  │ Python Portable Edition │ Praktis 100%, tanpa instalasi compiler, hemat memori HP│
│ ☁️ VPS / Server Linux   │ Hybrid Go + Rust Engine │ Konsumsi RAM 5-8 MB, zero GC lag, non-blocking polling │
│ 💻 Laptop / PC Desktop  │ Python / Hybrid Binary  │ Fleksibel untuk monitoring langsung saat jam kuliah    │
└─────────────────────────┴─────────────────────────┴────────────────────────────────────────────────────────┘
```

---

### Opsi A: Panduan Menjalankan di HP Android (Termux) — Rekomendasi Portabel

Jika ingin menjalankan bot langsung dari smartphone tanpa menyewa server:

1. **Instal Aplikasi Termux:**  
   Gunakan Termux versi resmi dari [F-Droid](https://f-droid.org/en/packages/com.termux/).
2. **Siapkan Paket Pendukung:**  
   Buka aplikasi Termux dan jalankan perintah berikut:
   ```bash
   pkg update && pkg install python git termux-api -y
   termux-wake-lock
   ```
   *(Penting: Setel pengaturan baterai aplikasi Termux di pengaturan Android menjadi "Tidak Dibatasi / Unrestricted" agar sistem tidak mematikan proses saat layar mati).*
3. **Clone Repositori & Pasang Dependensi:**
   ```bash
   git clone https://github.com/Gungna/KON-THOL.git
   cd KON-THOL
   pip install requests beautifulsoup4 urllib3
   ```
4. **Konfigurasi Akun:**
   ```bash
   python setup.py
   ```
5. **Jalankan Bot:**
   ```bash
   python ethol_autopresence.py
   ```

---

### Opsi B: Panduan Menjalankan di VPS / Server Linux (Debian / Ubuntu) — Rekomendasi Performa Tinggi (Go + Rust)

Untuk operasional stabil 24 jam nonstop dengan efisiensi sumber daya maksimal:

1. **Pasang Toolchain (Go & Rust):**
   ```bash
   sudo apt update && sudo apt install build-essential git -y
   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
   source $HOME/.cargo/env
   ```
2. **Kompilasi Core Rust:**
   ```bash
   cd core
   cargo build --release
   ```
3. **Kompilasi Binary Hybrid Go:**
   ```bash
   cd ..
   CGO_ENABLED=1 go build -ldflags="-s -w" -o konthol_hybrid main.go
   ```
4. **Jalankan sebagai Systemd Service (Otomatis Nyala Saat Reboot):**
   Buat berkas `/etc/systemd/system/konthol.service`:
   ```ini
   [Unit]
   Description=KON-THOL Hybrid Engine (Go + Rust)
   After=network.target network-online.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/opt/konthol-hybrid
   ExecStart=/opt/konthol-hybrid/konthol_hybrid -cred /opt/konthol-hybrid/credentials.json
   Restart=always
   RestartSec=5s

   [Install]
   WantedBy=multi-user.target
   ```
   Aktifkan service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now konthol.service
   ```

---

### Opsi C: Modul Multi-Account & WhatsApp Gateway

Bagi yang ingin memantau beberapa akun mahasiswa sekaligus dan mengirim notifikasi kehadiran langsung ke WhatsApp masing-masing:

1. Salin template konfigurasi:
   ```bash
   cp accounts.example.json accounts.json
   ```
2. Lengkapi konfigurasi WhatsApp (Fonnte / WPPConnect / Generic Webhook) dan daftar akun pada `accounts.json`.
3. Jalankan pemantauan multi-akun:
   - Menggunakan Python:
     ```bash
     python kon_thol_public.py
     ```
   - Menggunakan Hybrid Binary:
     ```bash
     ./konthol_hybrid -accounts accounts.json
     ```

---

## Struktur Berkas Proyek

```
KON-THOL/
├── core/                      # Rust Core Engine (Scraping, DOM parser, crypto in-memory)
│   ├── Cargo.toml
│   └── src/lib.rs
├── core_bridge/               # FFI Cgo Bridge penghubung Go dan Rust
│   └── core.go
├── dispatcher/                # Unified Dispatcher (WhatsApp, Termux, Windows Toast, Linux)
│   └── dispatcher.go
├── telegram/                  # Telegram Bot UI (2-page menu, 22 callbacks, loading bar)
│   └── bot.go
├── client/                    # Client HTTP API ETHOL & CAS SSO
│   └── client.go
├── config/                    # Manajemen kredensial & multi-account parser
│   └── config.go
├── ethol_autopresence.py      # Python V1 Public Edition (Portabel untuk Termux/Desktop)
├── kon_thol_public.py         # Python Multi-Account & WhatsApp Daemon
├── setup.py                   # Wizard interaktif setup kredensial akun & bot
├── credentials.json           # Berkas kredensial tersimpan lokal (jangan di-commit)
├── config.example.json        # Template manual konfigurasi kredensial
├── accounts.example.json      # Template multi-account & WhatsApp gateway
├── attended_keys.json         # Riwayat sesi presensi tercatat (auto-generated)
├── autopresence.log           # Log aktivitas sistem
├── assets/                    # Aset banner dan gambar bot
└── README.md                  # Panduan penggunaan
```

---

## Daftar Perintah

Perintah dapat diakses baik melalui pesan chat Telegram (*slash command*), tombol interaktif (*inline keyboard*), maupun langsung diketik di Terminal:

| Slash Command | Terminal CLI | Fungsi Utama |
| :--- | :--- | :--- |
| `/scan` atau `/absen` | `scan` | Memindai seluruh mata kuliah dan mengisi presensi yang sedang terbuka |
| `/jadwal` atau `/matkul` | `jadwal` | Menampilkan jadwal mingguan rapi + info dosen & status hadir hari ini |
| `/tugas` | `tugas` | Menampilkan tugas belum dikumpulkan beserta link pengumpulan portal ETHOL |
| `/rekap` | `rekap` | Statistik rekapitulasi kehadiran semester dan kehadiran per mata kuliah |
| `/log` | `log` | Menampilkan catatan riwayat log aktivitas sistem terbaru dengan filter kategori |
| `/status` | `status` | Ringkasan identitas mahasiswa, status sesi login SSO, dan status scanner |
| `/cooldown` | `cooldown` | Mengistirahatkan scanner presensi setelah kuliah hari ini selesai |
| `/resume` | `resume` | Membatalkan cooldown dan mengembalikan scanner ke mode siaga penuh |
| `/relogin` | `relogin` | Sinkronisasi ulang sesi autentikasi SSO PENS jika token kedaluwarsa |
| `/help` | `help` | Menampilkan ringkasan fungsi dan panduan perintah bot |

---

## Disclaimer & Orisinalitas Proyek

Seluruh baris kode, integrasi notifikasi, dan logika otomatisasi dalam proyek **KON-THOL** ini dirancang serta ditulis secara mandiri dari nol (*from scratch*) oleh pembuat melalui eksplorasi dan riset independen terhadap API portal E-THOL PENS. Proyek ini murni dibuat atas inisiatif pribadi tanpa pernah melihat, menyalin, ataupun mencontoh script dari pihak lain.

Oleh karena itu, apabila di kemudian hari terdapat script, bot, atau software pembantu presensi ETHOL lain yang beredar di kalangan mahasiswa dan memiliki kesamaan struktur logika atau kode, besar kemungkinan software tersebut berasal dari atau mengadopsi basis kode repositori ini.

> [!NOTE]
> **Catatan Penamaan Proyek:**  
> Penamaan akronim **KON-THOL** (*Kawan Otomasi dan Notifikasi untuk E-THOL*) dibuat semata-mata sebagai humor dan candaan ringan antar sesama mahasiswa. Kami menaruh rasa hormat yang setinggi-tingginya serta apresiasi yang tulus kepada segenap jajaran Sivitas Akademika PENS dan tim pengembang portal **E-THOL PENS** yang telah menghadirkan sistem perkuliahan digital yang luar biasa andal dan bermanfaat bagi kita semua.

Perangkat lunak ini dikembangkan secara independen sebagai asisten akademik pribadi nirlaba untuk mempermudah produktivitas belajar mahasiswa. Pengguna diharapkan tetap mematuhi seluruh peraturan, etika, dan tata tertib akademik yang berlaku di kampus. Harap menjaga kerahasiaan data akun Anda dan **JANGAN PERNAH** membagikan berkas `credentials.json` ke repositori publik.
