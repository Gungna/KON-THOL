<p align="center">
  <img src="assets/banner_github.jpg" alt="KON-THOL Banner" width="100%">
</p>

# KON-THOL 👀🐦
### "Kawan Otomasi dan Notifikasi untuk E-THOL PENS"

**KON-THOL** adalah bot asisten automasi presensi dan monitoring informasi E-THOL cerdas berbasis Python & Telegram Bot untuk mahasiswa Politeknik TERBAEKKK se-Asia Tenggara. Dirancang untuk mempermudah presensi perkuliahan, memeriksa jadwal harian & mingguan, merekap kehadiran, serta mengakses data portal tanpa hambatan keharusan login berulang kali—terutama saat portal sedang padat diakses.

> [!NOTE]
> **Efisiensi Beban Jaringan & Server:** Script ini beroperasi menggunakan arsitektur *session reuse*, caching ringan, dan siklus *adaptive multi-tier polling* yang dirancang hemat resource. Proses pemindaian hanya mengirimkan permintaan HTTP berukuran sangat kecil secara berkala, sehingga berjalan efisien tanpa membebani *traffic* ataupun menimbulkan kelambatan pada infrastruktur portal E-THOL PENS.

---

## Pilihan Metode Pengoperasian

Sistem dapat dijalankan secara fleksibel sesuai infrastruktur yang tersedia:

1. **Terintegrasi dengan Bot Telegram (Pilihan Utama)**:
   - Seluruh kontrol dan pemantauan berada langsung di aplikasi Telegram (smartphone / desktop).
   - Notifikasi pengisian presensi, tugas baru, dan pengingat jadwal dikirimkan secara instan.
   - Mendukung kontrol multi-akun dengan pemisahan hak akses (Admin & Member).
   - Seluruh perintah dapat diakses cepat melalui tombol menu interaktif maupun *slash command*.
2. **Berjalan Mandiri dari Terminal / CLI (Laptop / Termux)**:
   - Dapat dijalankan secara lokal tanpa menghubungkan bot Telegram.
   - **Di Android (Termux):** Notifikasi sistem dikirim ke status bar perangkat melalui integrasi `termux-api`.
   - **Di Terminal PC/Linux:** Notifikasi serta status proses dicetak rapi secara real-time di layar konsol.
   - **Mode Interaktif CLI:** Perintah dapat diketik langsung di terminal seperti `scan`, `jadwal`, `tugas`, `rekap`, `log`, `status`, `cooldown`, atau `resume`.

> [!IMPORTANT]
> **Catatan Stabilitas Operasional:** Pengoperasian di smartphone Android (Termux) atau laptop pribadi berpotensi terhenti sewaktu-waktu akibat manajemen penghemat daya (*battery saver*) atau mode sleep perangkat. Untuk operasional presensi yang **stabil, konsisten, dan teruji 24/7**, disarankan menjalankan bot di server Linux / VPS mandiri yang terhubung ke Bot Telegram.

---

## Fitur Utama

- **Pemindaian & Otomasi Presensi Cepat**:  
  Mendeteksi dan menyelesaikan sesi presensi secara otomatis saat dibuka oleh dosen pengampu, memastikan kehadiran tercatat tepat waktu tanpa mengganggu konsentrasi belajar.
- **Arsitektur Multi-Akun (Admin & Member Space)**:  
  Mendukung pengelolaan lebih dari satu akun mahasiswa dalam satu sistem bot dengan pembagian ruang kerja:
  - **Tampilan Admin (Pemilik Bot)**: Dashboard terpusat untuk memantau status operasional seluruh akun (`/status`), perbandingan rekapitulasi kehadiran (`/rekap all` atau `/rekap [urutan]`), agenda kuliah harian (`/jadwal all` atau `/jadwal [urutan]`), daftar tugas gabungan (`/tugas all`), serta pemindaian serentak (`/scan all`).
  - **Tampilan Member (Mahasiswa Tertaut)**: Ruang privat terlindungi. Mahasiswa yang menautkan Telegram Chat ID hanya dapat mengakses jadwal, tugas, rekapitulasi, dan status presensi akun miliknya sendiri tanpa melihat data mahasiswa lain.
  - **Single Account Mode**: Bila hanya terdapat 1 akun terdaftar, menu otomatis beroperasi dalam format personal tunggal yang ringkas.
- **Antarmuka Bersih & Otomatis (Single-View Clean UI)**:  
  Ruang obrolan Telegram tetap rapi layaknya antarmuka aplikasi. Pesan perintah dan interaksi sementara dibersihkan otomatis, menyisakan Banner Menu utama di bagian atas dan respon data terbaru di bawahnya.
- **Siklus Operasional Berdasarkan Waktu Perkuliahan**:  
  - 🌅 **Siaga Subuh (04:00 - 06:30 WIB)**: Bersiap memantau jadwal dan persiapan sesi perkuliahan sejak dini hari sebelum azan Subuh berkumandang.  
  - 🟢 **Siaga Penuh (06:30 - 21:30 WIB)**: Siklus pemindaian presensi dan pemantauan tugas aktif penuh pada jam operasional kuliah reguler.  
  - 💤 **Istirahat Malam (21:30 - 04:00 WIB)**: Menjeda pemindaian saat malam hari demi efisiensi resource dan menjaga etika beban server.
- **Jadwal Kuliah Komprehensif**:  
  Menyajikan jadwal kuliah mingguan terurut hari dan jam, dilengkapi informasi ruang perkuliahan, nama dosen pengampu, serta status kehadiran mata kuliah hari ini.
- **Pemantau Tugas & Tautan Langsung Portal**:  
  Menampilkan daftar tugas yang belum dikumpulkan, batas waktu pengumpulan (*deadline*), serta tautan langsung menuju halaman pengumpulan di portal ETHOL.
- **Rekapitulasi Kehadiran Akurat**:  
  Menghitung persentase kehadiran semester berjalan dan rincian kehadiran per mata kuliah berdasarkan data portal resmi.
- **Pengecekan Notifikasi Portal**:  
  Mengambil umpan notifikasi terbaru dari portal akademik secara berkala.
- **Pencatatan Log Transparan**:  
  Menampilkan catatan riwayat aktivitas sistem terkini (via `/log` atau CLI `log`) untuk memudahkan pemantauan proses tanpa membuka berkas log secara manual.
- **Mode Cooldown & Siaga Manual**:  
  Mendukung pengistirahatan proses scanner secara manual saat seluruh kuliah selesai (`/cooldown`) dan mengaktifkannya kembali kapan saja (`/resume`).

---

## Pengelolaan Akun (Multi-Account)

Pengelolaan akun dapat dilakukan langsung melalui antarmuka Telegram oleh Admin bot:

| Perintah | Format Input | Deskripsi |
| :--- | :--- | :--- |
| `/accounts` | `/accounts` | Menampilkan seluruh daftar akun terdaftar beserta status login dan tautan Telegram ID |
| `/addaccount` | `/addaccount Nama \| email \| password [\| telegram_chat_id]` | Mendaftarkan akun mahasiswa baru ke sistem bot |
| `/delaccount` | `/delaccount [urutan_akun / email / telegram_id]` | Menghapus akun terdaftar menggunakan nomor urut (contoh: `/delaccount 2`), email, atau ID Telegram |

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

1. Pasang aplikasi **Termux** (disarankan via [F-Droid](https://f-droid.org/en/packages/com.termux/)).
2. Buka Termux, lalu jalankan:
   ```bash
   pkg update && pkg install python git termux-api -y
   termux-wake-lock
   ```
   *(Pastikan pengaturan manajemen baterai aplikasi Termux di smartphone disetel ke "Tidak Dibatasi / Unrestricted").*
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

1. Pastikan **Python 3.10+** telah terpasang.
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
├── credentials.json           # Berkas kredensial aktif lokal (JANGAN DI-COMMIT)
├── accounts.json              # Berkas data multi-akun aktif lokal (JANGAN DI-COMMIT)
├── attended_keys.json         # Riwayat kunci presensi terverifikasi (auto-generated)
├── autopresence.log           # Berkas catatan log aktivitas sistem
├── assets/                    # Direktori aset gambar dan banner bot
└── README.md                  # Dokumentasi teknis
```

---

## Daftar Perintah Lengkap

| Slash Command (Telegram) | Perintah Terminal (CLI) | Deskripsi Fungsi |
| :--- | :--- | :--- |
| `/scan` [all / urutan] | `scan` | Memindai dan mengisi presensi yang aktif (mendukung pemindaian per akun atau serentak) |
| `/jadwal` [all / urutan] | `jadwal` | Menampilkan jadwal kuliah mingguan, info dosen, dan status presensi hari ini |
| `/tugas` [all / urutan] | `tugas` | Menampilkan tugas aktif, sisa batas waktu, dan tautan portal pengumpulan |
| `/rekap` [all / urutan] | `rekap` | Menampilkan rekapitulasi persentase kehadiran semester dan mata kuliah |
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

## Disclaimer & Batasan Tanggung Jawab

> [!NOTE]
> **Catatan Penamaan Proyek:**  
> Penamaan akronim **KON-THOL** (*Kawan Otomasi dan Notifikasi untuk E-THOL*) dibuat semata-mata sebagai humor dan candaan ringan antar sesama mahasiswa. Kami menaruh rasa hormat yang setinggi-tingginya serta apresiasi yang tulus kepada segenap jajaran Sivitas Akademika PENS dan tim pengembang portal **E-THOL PENS** yang telah menghadirkan sistem perkuliahan digital yang luar biasa andal dan bermanfaat bagi seluruh civitas akademika.

> [!CAUTION]
> **PERINGATAN KEAMANAN & BATASAN TANGGUNG JAWAB PENGEMBANG:**
> 1. Berkas kredensial (`credentials.json`, `accounts.json`, password SSO, maupun token bot Telegram) memuat data autentikasi pribadi yang sangat sensitif. **JANGAN PERNAH** membagikan, mengunggah, atau melakukan `git commit / push` berkas-berkas kredensial ke repositori publik atau kepada pihak mana pun.
> 2. Segala bentuk tindakan, kelalaian, kebocoran akun akibat kecerobohan pengguna, kendala teknis, maupun konsekuensi akademik apa pun yang timbul dari pengoperasian perangkat lunak ini adalah **SEPENUHNYA MENJADI TANGGUNG JAWAB PRIBADI MASING-MASING PENGGUNA**. Creator / pengembang perangkat lunak ini **TIDAK BERTANGGUNG JAWAB ATAS SEGALA BENTUK KONSEKUENSI MAUPUN DAMPAK APA PUN** yang dialami oleh pengguna maupun pihak lain akibat penggunaan script ini.
