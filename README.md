<p align="center">
  <img src="assets/banner_github.jpg" alt="KON-THOL Banner" width="100%">
</p>

# KON-THOL 👀🐦
### "Kawan Otomasi dan Notifikasi untuk E-THOL PENS"

**KON-THOL** adalah bot asisten automasi dan monitoring perkuliahan cerdas berbasis Python & Telegram Bot untuk mahasiswa PENS (Politeknik Elektronika Negeri Surabaya). Dirancang untuk membantu mahasiswa memantau sesi presensi perkuliahan, memeriksa jadwal kuliah harian & mingguan, merekap tingkat kehadiran, serta mencatat tenggat tugas secara terstruktur.

---

## Pilihan Metode Pengoperasian

Bot **KON-THOL** dapat dioperasikan secara fleksibel sesuai infrastruktur yang Anda miliki:

1. **Terintegrasi dengan Bot Telegram (Rekomendasi Utama)**:
   - Seluruh kontrol berada langsung di aplikasi Telegram (smartphone / desktop).
   - Notifikasi pengisian presensi, tugas baru, dan pengingat jadwal dikirimkan secara instan.
   - Mendukung kontrol multi-akun dengan pembagian hak akses (Admin & Member).
   - Semua perintah dapat diakses cepat menggunakan *slash command* standar.
2. **Berjalan Mandiri dari Terminal / CLI (Laptop / Termux)**:
   - Dapat dijalankan langsung tanpa bot Telegram jika hanya dibutuhkan di lingkungan lokal.
   - **Di Android (Termux):** Notifikasi sistem dikirim ke status bar perangkat menggunakan paket `termux-api`.
   - **Di Terminal PC/Linux:** Notifikasi dan log dicetak rapi secara real-time di layar konsol.
   - **Mode Interaktif CLI:** Anda dapat mengetikkan perintah langsung di terminal seperti `scan`, `jadwal`, `tugas`, `rekap`, `log`, `status`, `cooldown`, atau `resume`.

> [!IMPORTANT]
> **Catatan Stabilitas Operasional:** Penggunaan di smartphone Android (Termux) atau laptop pribadi dapat terhenti sewaktu-waktu akibat manajemen daya baterai background (*battery saver*) atau mode sleep. Untuk operasional presensi yang **stabil, konsisten, dan teruji 24/7**, sangat disarankan menjalankan bot di server Linux / VPS mandiri yang terhubung ke Bot Telegram pribadi Anda.

---

## Fitur Utama

- **Pemindaian & Otomasi Presensi Cepat**:  
  Mendeteksi dan menyelesaikan sesi presensi secara otomatis begitu dibuka oleh dosen pengampu, memastikan kehadiran tercatat tepat waktu tanpa mengganggu konsentrasi belajar.
- **Arsitektur Multi-Akun Cerdas (Admin & Member Space)**:  
  Mendukung pengelolaan lebih dari satu akun mahasiswa dalam satu bot yang sama dengan pemisahan hak akses:
  - **Tampilan Admin (Pemilik Bot)**: Dashboard terpusat untuk memantau status semua akun (`/status`), perbandingan rekapitulasi kehadiran (`/rekap all` atau `/rekap [urutan]`), agenda kuliah hari ini (`/jadwal all` atau `/jadwal [urutan]`), tugas gabungan (`/tugas all`), serta pemindaian serentak (`/scan all`).
  - **Tampilan Member (Mahasiswa Tertaut)**: Ruang privat terlindungi. Mahasiswa lain yang menautkan Telegram Chat ID-nya hanya dapat mengakses jadwal, tugas, rekap, dan status presensi miliknya sendiri tanpa melihat data mahasiswa lain.
  - **Single Account Mode**: Bila hanya terdapat 1 akun terdaftar, menu otomatis beroperasi dalam format personal tunggal yang ringkas.
- **Antarmuka Rapi & Otomatis (Single-View Clean UI)**:  
  Ruang obrolan Telegram tetap bersih seperti aplikasi native. Pesan command dan interaksi sementara dibersihkan secara otomatis, menyisakan Banner Menu utama di bagian atas dan respon data terbaru di bawahnya.
- **Siklus Operasional Berdasarkan Waktu Perkuliahan**:  
  - 🌅 **Siaga Subuh (04:00 - 06:30 WIB)**: Bersiap memantau jadwal dan persiapan sesi kuliah hari ini sejak dini hari sebelum adzan Subuh.  
  - 🟢 **Siaga Penuh (06:30 - 21:30 WIB)**: Siklus pemindaian presensi dan pemantauan tugas aktif penuh pada jam operasional kuliah.  
  - 💤 **Istirahat Malam (21:30 - 04:00 WIB)**: Menjeda polling agresif saat malam hari untuk efisiensi resource dan menjaga etika beban server kampus.
- **Jadwal Kuliah Komprehensif**:  
  Menyajikan jadwal kuliah mingguan terurut hari dan jam, dilengkapi ruang kuliah, nama dosen pengampu, serta status kehadiran matkul hari ini.
- **Pemantau Tugas & Tautan Langsung Portal**:  
  Menampilkan daftar tugas yang belum dikumpulkan, batas waktu pengumpulan (*deadline*), serta link langsung untuk membuka halaman submit di portal ETHOL.
- **Rekapitulasi Kehadiran Akurat**:  
  Menghitung persentase kehadiran semester berjalan dan rincian kehadiran per mata kuliah berdasarkan data portal resmi.
- **Notifikasi Portal Terkini**:  
  Mengambil umpan notifikasi terbaru dari portal akademik secara berkala.
- **Pencatatan Log Transparan**:  
  Menampilkan catatan riwayat aktivitas sistem terkini (via `/log` atau CLI `log`) untuk verifikasi pemindaian tanpa perlu membuka file log manual.
- **Mode Cooldown & Siaga Manual**:  
  Mendukung pengistirahatan proses scanner secara manual saat seluruh kuliah hari ini selesai (`/cooldown`) dan mengaktifkannya kembali kapan saja (`/resume`).

---

## Panduan Pengelolaan Akun (Multi-Account)

Pengelolaan akun dapat dilakukan langsung melalui antarmuka Telegram oleh Admin bot:

| Perintah | Format Input | Deskripsi |
| :--- | :--- | :--- |
| `/accounts` | `/accounts` | Menampilkan seluruh daftar akun terdaftar beserta status login dan tautan Telegram ID |
| `/addaccount` | `/addaccount Nama \| email \| password [\| telegram_chat_id]` | Mendaftarkan akun mahasiswa baru ke sistem bot |
| `/delaccount` | `/delaccount [urutan_akun / email / telegram_id]` | Menghapus akun dari sistem menggunakan nomor urut (contoh: `/delaccount 2`), email, atau ID Telegram |

---

## Panduan Pemasangan (Instalasi)

### Metode 1: Instalasi di VPS / Server Linux (Rekomendasi 24/7)

1. **Perbarui sistem dan pasang dependensi:**
   ```bash
   sudo apt update && sudo apt install python3 python3-pip git -y
   ```
2. **Klon repositori:**
   ```bash
   git clone https://github.com/Gungna/KON-THOL.git /opt/ethol-autopresence
   cd /opt/ethol-autopresence
   pip3 install requests beautifulsoup4 urllib3
   ```
3. **Jalankan konfigurasi awal:**
   ```bash
   python3 setup.py
   ```
4. **Jalankan bot:**
   ```bash
   python3 ethol_autopresence.py
   ```

---

### Metode 2: Instalasi di HP Android (Termux)

1. Pasang aplikasi **Termux** (disarankan unduh via [F-Droid](https://f-droid.org/en/packages/com.termux/)).
2. Buka Termux, lalu jalankan:
   ```bash
   pkg update && pkg install python git termux-api -y
   termux-wake-lock
   ```
   *(Pastikan pengaturan manajemen baterai aplikasi Termux di smartphone Anda disetel ke "Tidak Dibatasi / Unrestricted").*
3. Klon repositori dan pasang pustaka:
   ```bash
   git clone https://github.com/Gungna/KON-THOL.git
   cd KON-THOL
   pip install requests beautifulsoup4 urllib3
   ```
4. Jalankan konfigurasi dan aplikasi:
   ```bash
   python setup.py
   python ethol_autopresence.py
   ```

---

### Metode 3: Instalasi di Laptop / PC Pribadi (Windows / macOS)

1. Pastikan **Python 3.10+** telah terpasang di perangkat Anda.
2. Buka Terminal / PowerShell di dalam folder proyek, lalu pasang dependensi:
   ```bash
   pip install requests beautifulsoup4 urllib3
   ```
3. Jalankan wizard konfigurasi dan mulai bot:
   ```bash
   python setup.py
   python ethol_autopresence.py
   ```

---

## Struktur Berkas Repositori

```
KON-THOL/
├── ethol_autopresence.py      # Engine utama presensi, Telegram bot, dan multi-akun
├── setup.py                   # Wizard interaktif konfigurasi awal akun & bot
├── config.example.json        # Template struktur kredensial akun tunggal
├── accounts.example.json      # Template struktur kredensial multi-akun
├── credentials.json           # File kredensial aktif lokal (JANGAN DI-COMMIT)
├── accounts.json              # File data multi-akun aktif lokal (JANGAN DI-COMMIT)
├── attended_keys.json         # Riwayat kunci presensi terverifikasi (auto-generated)
├── autopresence.log           # Berkas log aktivitas sistem
├── assets/                    # Direktori aset gambar dan banner bot
└── README.md                  # Dokumentasi panduan teknis
```

---

## Daftar Perintah Lengkap

| Slash Command (Telegram) | Perintah Terminal (CLI) | Deskripsi Fungsi |
| :--- | :--- | :--- |
| `/scan` [all / urutan] | `scan` | Memindai dan mengisi presensi yang aktif (mendukung pemindaian per akun atau serentak) |
| `/jadwal` [all / urutan] | `jadwal` | Menampilkan jadwal kuliah mingguan, info dosen, dan status presensi hari ini |
| `/tugas` [all / urutan] | `tugas` | Menampilkan tugas aktif, sisa batas waktu, dan link portal pengumpulan |
| `/rekap` [all / urutan] | `rekap` | Menampilkan rekapitulasi persentase kehadiran per semester dan mata kuliah |
| `/accounts` | `accounts` | Melihat daftar seluruh akun mahasiswa yang terdaftar pada engine |
| `/addaccount` | - | Menambahkan akun mahasiswa baru ke sistem |
| `/delaccount` | - | Menghapus akun terdaftar berdasarkan urutan (contoh: `/delaccount 2`) atau email |
| `/status` | `status` | Ringkasan operasional bot, status login SSO, dan status scanner |
| `/cooldown` | `cooldown` | Mengistirahatkan polling presensi harian setelah kuliah selesai |
| `/resume` | `resume` | Membatalkan cooldown dan mengembalikan scanner ke mode siaga penuh |
| `/log` | `log` | Menampilkan 15 baris log aktivitas sistem terbaru |
| `/relogin` | `relogin` | Memperbarui sesi autentikasi SSO secara manual jika token kedaluwarsa |
| `/help` | `help` | Menampilkan ringkasan menu bantuan dan panduan perintah |

---

## Orisinalitas Proyek & Hak Cipta Logika

Seluruh baris kode, arsitektur integrasi, algoritma multi-tier polling, dan alur automasi dalam proyek **KON-THOL** ini dirancang serta ditulis secara orisinal dari nol (*from scratch*) oleh pembuat melalui riset independen terhadap alur kerja sistem perkuliahan digital E-THOL PENS. Proyek ini dibangun atas inisiatif pribadi tanpa pernah meniru, menyalin, atau mengadopsi basis kode dari pihak mana pun.

Oleh karena itu, apabila di kemudian hari terdapat script, bot, atau software pembantu presensi ETHOL lain yang beredar di kalangan mahasiswa dan memiliki kesamaan struktur logika atau kode (meskipun menggunakan bahasa pemograman yang berbeda), besar kemungkinan perangkat lunak tersebut berasal dari atau mengadopsi basis logika dan karya cipta dari repositori ini.

---

## Peringatan Keamanan & Batasan Tanggung Jawab (Disclaimer)

> [!CAUTION]
> **PERINGATAN KERAS MENGENAI KEAMANAN DATA PRIBADI:**
> 1. Berkas kredensial (`credentials.json`, `accounts.json`, password SSO, maupun token bot Telegram) memuat data otentikasi pribadi Anda yang sangat sensitif.
> 2. **JANGAN PERNAH** mengunggah, membagikan, atau melakukan `git commit / push` berkas-berkas kredensial tersebut ke repositori publik, forum, atau kepada pihak ketiga mana pun.
> 3. Pastikan berkas-berkas kredensial selalu tercantum di dalam `.gitignore` di perangkat Anda.

Perangkat lunak ini dikembangkan secara independen sebagai alat bantu produktivitas dan asisten akademik pribadi yang bersifat nirlaba. Pengguna diwajibkan untuk tetap mematuhi seluruh etika, tata tertib, dan regulasi akademik yang berlaku di lingkungan kampus.

**BATASAN TANGGUNG JAWAB PENGEMBANG:**  
Segala bentuk penggunaan, tindakan, kelalaian, dampak langsung maupun tidak langsung, kendala teknis, kebocoran kredensial akibat kelalaian pengguna, maupun konsekuensi akademik apa pun yang timbul dari pengoperasian perangkat lunak ini adalah **SEPENUHNYA MERUPAKAN TANGGUNG JAWAB PRIBADI MASING-MASING PENGGUNA**. Creator / pengembang perangkat lunak ini **TIDAK BERTANGGUNG JAWAB ATAS SEGALA BENTUK KONSEKUENSI APA PUN** yang dialami oleh pengguna maupun pihak lain akibat penggunaan script ini.
