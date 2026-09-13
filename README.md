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

## Panduan Integrasi Telegram Bot (Dari Awal Sampai Selesai)

Setiap pengguna membuat dan menggunakan Bot Telegram pribadi secara mandiri tanpa biaya. Berikut alur penyiapan dari awal:

1. **Pembuatan Bot di Telegram**:
   - Buka aplikasi Telegram, cari akun resmi **@BotFather**, lalu tekan tombol Start.
   - Kirim perintah `/newbot`.
   - Masukkan nama tampilan bot yang diinginkan (contoh: `Asisten Presensi PENS`).
   - Masukkan username bot unik yang berakhiran kata `bot` (contoh: `pens_presensi_robot`).
   - BotFather akan memberikan **HTTP API Token** (contoh format: `7123456789:AAFxX...`). Simpan token ini dengan aman.
2. **Mendapatkan Telegram Chat ID**:
   - Cari akun bot **@userinfobot** di Telegram, lalu tekan tombol Start.
   - Bot akan membalas dengan menampilkan nomor ID Telegram pengguna (berupa deretan angka).
3. **Mengaktifkan Izin Pesan Bot**:
   - Cari username bot pribadi yang baru saja dibuat di langkah nomor 1.
   - Tekan tombol **Start** atau kirim pesan `/start` ke bot tersebut agar bot memiliki izin mengirimkan notifikasi.
4. **Penyimpanan Konfigurasi**:
   - Jalankan wizard otomatis:
     ```bash
     python setup.py
     ```
   - Masukkan email SSO PENS, password SSO, Token Bot Telegram, serta Chat ID saat diminta. Seluruh konfigurasi akan tersimpan otomatis di berkas lokal `credentials.json`.
   - Atau buat manual berkas `credentials.json` sesuai template `config.example.json`:
     ```json
     {
       "pens_email": "nama_mahasiswa@it.student.pens.ac.id",
       "pens_password": "PASSWORD_SSO_ANDA",
       "tg_token": "TOKEN_DARI_BOTFATHER",
       "tg_chat_id": "CHAT_ID_DARI_USERINFOBOT",
       "tg_allowed_users": ["CHAT_ID_DARI_USERINFOBOT"]
     }
     ```

---

## Fitur Utama

- **Otomasi Presensi Tenang & Cepat**:  
  Memantau dan mengisi presensi secara otomatis ketika sesi kuliah dibuka oleh dosen, sehingga Anda tidak perlu cemas terlewat sesi presensi saat sedang fokus menyimak materi.
- **Antarmuka Rapi & Otomatis (Single-View Clean UI)**:  
  Chat Telegram tetap bersih dan rapi layaknya dashboard aplikasi. Perintah yang telah usang beserta pesan interaksi perantara otomatis dibersihkan dari ruang chat, menyisakan Banner Menu utama di atas dan hasil perintah terbaru Anda di bawahnya tanpa pernah muncul dobel.
- **In-Place Cooldown & Resume UX**:  
  Mengaktifkan mode cooldown atau siaga penuh langsung memperbarui status menu di tempat disertai umpan balik *callback alert* dan status `Command : Success`, tanpa kartu perantara yang mengharuskan klik kembali.
- **Transisi Visual Progresif (Adaptive Loading Bar)**:  
  Umpan balik seketika yang adaptif sesuai kedalaman proses: 1 langkah langsung terbuka seketika (100%), 2 langkah untuk scan presensi (50% -> 100%), dan 3 langkah untuk pemindaian paralel multi-akun (33% -> 66% -> 100%).
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
  Mendukung pemindaian dan pengisian presensi otomatis untuk 2 atau lebih akun mahasiswa sekaligus secara serentak (*concurrent multithreading*) menggunakan `ThreadPoolExecutor`, lengkap dengan notifikasi WhatsApp Gateway real-time (Fonnte, WAHA, Evolution API, atau Webhook) serta fallback Telegram.

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

### Opsi A: Panduan Menjalankan di HP Android (Termux) - Rekomendasi Portabel

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

### Opsi B: Panduan Menjalankan di VPS / Server Linux (Debian / Ubuntu) - Rekomendasi Performa Tinggi (Go + Rust)

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
2. Lengkapi konfigurasi WhatsApp (Fonnte / WAHA / Evolution API / Webhook) dan daftar akun pada `accounts.json`.
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

## Panduan Integrasi WhatsApp Gateway (Dari Awal Sampai Selesai)

Untuk pengiriman notifikasi presensi otomatis melalui WhatsApp ke nomor pribadi maupun rekan kelompok, sistem menyediakan integrasi gateway yang mendukung berbagai penyedia:

### Pilihan 1: Menggunakan Layanan Cloud Gateway (Fonnte)
1. Buka situs penyedia gateway **Fonnte** (fonnte.com) dan lakukan pendaftaran akun.
2. Masuk ke dashboard Fonnte, lalu hubungkan nomor WhatsApp pengirim dengan memindai kode QR pada menu *Perangkat Tertaut* di aplikasi WhatsApp smartphone.
3. Buka menu *API Token* di dashboard Fonnte, lalu salin token yang tersedia.
4. Buka berkas `accounts.json` (salinan dari `accounts.example.json`), lalu sesuaikan bagian konfigurasi:
   ```json
   "whatsapp": {
       "provider": "fonnte",
       "api_key": "MASUKKAN_TOKEN_FONNTE_DISINI",
       "target_phone": "6281234567890",
       "endpoint_url": "https://api.fonnte.com/send"
   }
   ```

### Pilihan 2: Menggunakan Gateway Mandiri - WAHA (WhatsApp HTTP API)
WAHA adalah gateway WhatsApp mandiri berbasis Docker yang sangat stabil dan hemat memori:
1. Jalankan container WAHA di server atau komputer Anda:
   ```bash
   docker run -d --name waha -p 3000:3000 -v waha_sessions:/app/.sessions --restart always devlikeappro/waha
   ```
2. Buka dashboard WAHA di browser (`http://IP_SERVER:3000/dashboard`).
3. Mulai session (misalnya session `default`), lalu pindai kode QR menggunakan WhatsApp di smartphone.
4. Buka berkas `accounts.json`, lalu atur bagian WhatsApp:
   ```json
   "whatsapp": {
       "provider": "waha",
       "api_key": "API_KEY_JIKA_DIATUR",
       "session_name": "default",
       "target_phone": "6281234567890",
       "endpoint_url": "http://IP_SERVER:3000/api/sendText"
   }
   ```

### Pilihan 3: Menggunakan Gateway Mandiri - Evolution API (Evo)
Evolution API adalah engine WhatsApp multi-device canggih berbasis Node.js:
1. Pasang dan jalankan instance Evolution API (melalui Docker Compose atau binary):
   ```bash
   docker run -d --name evo -p 8080:8080 -e AUTHENTICATION_API_KEY=KUNCI_API_ANDA atendai/evolution-api:v2.1.1
   ```
2. Buat instance baru melalui API atau panel manager Evolution (contoh nama instance: `konthol`).
3. Pindai kode QR instance untuk menautkan nomor WhatsApp pengirim.
4. Buka berkas `accounts.json`, lalu atur konfigurasi:
   ```json
   "whatsapp": {
       "provider": "evolution",
       "api_key": "KUNCI_API_ANDA",
       "session_name": "konthol",
       "target_phone": "6281234567890",
       "endpoint_url": "http://IP_SERVER:8080/message/sendText/konthol"
   }
   ```

### Pilihan 4: Menggunakan Custom Webhook / WPPConnect
Jika Anda menggunakan webhook custom atau microservice bot WhatsApp lainnya:
```json
"whatsapp": {
    "provider": "webhook",
    "api_key": "BEARER_TOKEN_JIKA_ADA",
    "target_phone": "6281234567890",
    "endpoint_url": "http://IP_SERVER_GATEWAY:PORT/api/sendText"
}
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
├── dispatcher/                # Unified Dispatcher (WAHA, Evolution API, Fonnte, Termux, Windows Toast, Linux)
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
