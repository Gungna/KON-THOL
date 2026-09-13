package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"konthol/client"
	"konthol/config"
	"konthol/telegram"
)

const LogFileName = "autopresence.log"

type BotState struct {
	CredPath       string
	AccPath        string
	StatePath      string
	LogPath        string
	BannerPaths    []string
	Creds          *config.Credentials
	Accounts       *config.AccountsConfig
	PrimaryClient  *client.EtholClient
	TgBot          *telegram.TelegramBot
	CooldownActive bool
	CooldownDate   string
	ForceSiaga     bool
	AttendedKeys   map[string]bool
	LastScanTime   time.Time
	StartTime      time.Time
	mu             sync.RWMutex
}

var state BotState

func main() {
	credFlag := flag.String("cred", "credentials.json", "Path to credentials.json")
	accFlag := flag.String("accounts", "accounts.json", "Path to accounts.json")
	stateFlag := flag.String("state", "attended_keys.json", "Path to attended_keys.json")
	flag.Parse()

	log.Println("[INIT] Memulai KON-THOL Engine (Modern Go Architecture)...")
	state.StartTime = time.Now()
	state.CredPath = *credFlag
	state.AccPath = *accFlag
	state.StatePath = *stateFlag
	state.LogPath = LogFileName
	state.AttendedKeys = make(map[string]bool)
	state.BannerPaths = []string{
		"banner.jpg",
		"assets/banner.jpg",
		"/opt/ethol-autopresence/banner.jpg",
		"/opt/konthol-go/banner.jpg",
	}

	loadAttendedKeys()

	// 1. Load Credentials
	creds, err := config.LoadCredentials(state.CredPath)
	if err != nil {
		log.Fatalf("[FATAL] Gagal membaca %s: %v", state.CredPath, err)
	}
	state.Creds = creds

	// 2. Load Accounts
	if accs, err := config.LoadAccounts(state.AccPath); err == nil {
		state.Accounts = accs
		log.Printf("[CONFIG] Multi-akun terdeteksi: %d akun mahasiswa", len(accs.Accounts))
	}

	// 3. Init Ethol Client
	etholCl, err := client.NewEtholClient(creds.Username, creds.Password)
	if err != nil {
		log.Fatalf("[FATAL] Inisialisasi client ETHOL gagal: %v", err)
	}
	state.PrimaryClient = etholCl

	// 4. Inisialisasi Telegram Bot SEGERA (Non-Blocking)
	// Memastikan bot langsung responsif terhadap perintah pengguna tanpa tertahan koneksi SSO
	if creds.TelegramToken != "" {
		tg := telegram.NewTelegramBot(creds.TelegramToken, creds.TelegramChatID)
		state.TgBot = tg
		tg.CommandHandler = handleTgCommand
		tg.ActionHandler = handleTgAction

		stopChan := make(chan struct{})
		go tg.StartPolling(stopChan)

		// Kirim menu utama awal
		sendWelcomeMenu()
	}

	// 5. Otentikasi CAS SSO di Goroutine Background
	go func() {
		appendFileLog("[INFO] Menghubungkan otentikasi CAS SSO PENS di background...")
		ok, err := etholCl.LoginCAS()
		if !ok || err != nil {
			appendFileLog(fmt.Sprintf("[ERROR] Login CAS awal terkendala: %v", err))
		} else {
			appendFileLog(fmt.Sprintf("[INFO] Login CAS Berhasil: %s (%s)", etholCl.UserInfo.Nama, etholCl.UserInfo.NipNrp))
			_ = etholCl.UpdateCache(true)
			sendWelcomeMenu()
		}
	}()

	// 6. Start Polling Engine Loop
	stopLoop := make(chan struct{})
	go runSchedulerLoop(stopLoop)

	appendFileLog("[INFO] Bot Polling aktif (Store-like V2 UI)...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[SYSTEM] Sinyal terminate diterima. Menyimpan state dan keluar...")
	saveAttendedKeys()
	close(stopLoop)
	time.Sleep(500 * time.Millisecond)
	log.Println("[SYSTEM] Proses selesai.")
}

func getWIBNow() time.Time {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.Now().UTC().Add(7 * time.Hour)
	}
	return time.Now().In(loc)
}

func getWIBStr() string {
	return getWIBNow().Format("02-01-2006 15:04:05 WIB")
}

func appendFileLog(line string) {
	entry := fmt.Sprintf("%s WIB %s\n", getWIBNow().Format("2006-01-02 15:04:05"), line)
	f, err := os.OpenFile(state.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.WriteString(entry)
		f.Close()
	}
	log.Print(line)
}

func loadAttendedKeys() {
	data, err := os.ReadFile(state.StatePath)
	if err != nil {
		return
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err == nil {
		state.mu.Lock()
		for _, k := range keys {
			state.AttendedKeys[k] = true
		}
		state.mu.Unlock()
	}
}

func saveAttendedKeys() {
	state.mu.RLock()
	keys := make([]string, 0, len(state.AttendedKeys))
	for k := range state.AttendedKeys {
		keys = append(keys, k)
	}
	state.mu.RUnlock()

	data, err := json.MarshalIndent(keys, "", "  ")
	if err == nil {
		_ = os.WriteFile(state.StatePath, data, 0644)
	}
}

func isCooldownActiveToday() bool {
	if !state.CooldownActive {
		return false
	}
	today := getWIBNow().Format("2006-01-02")
	return state.CooldownDate == today
}

func runSchedulerLoop(stopChan <-chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		default:
			interval := getNextInterval()
			time.Sleep(interval)

			state.mu.RLock()
			isCooldown := isCooldownActiveToday()
			needLogin := (state.PrimaryClient.UserInfo == nil)
			state.mu.RUnlock()

			if needLogin {
				ok, err := state.PrimaryClient.LoginCAS()
				if ok {
					appendFileLog(fmt.Sprintf("[INFO] Auto re-login CAS Berhasil: %s (%s)", state.PrimaryClient.UserInfo.Nama, state.PrimaryClient.UserInfo.NipNrp))
					_ = state.PrimaryClient.UpdateCache(true)
					sendWelcomeMenu()
				} else {
					appendFileLog(fmt.Sprintf("[WARN] Percobaan auto re-login terkendala: %v", err))
				}
			}

			if isCooldown {
				continue
			}

			scanActiveAttendance(false)
		}
	}
}

func getNextInterval() time.Duration {
	now := getWIBNow()
	hour := now.Hour()
	minute := now.Minute()
	timeVal := float64(hour) + float64(minute)/60.0

	if hour >= 21 || hour < 4 {
		return 15 * time.Minute
	}
	if timeVal >= 4.0 && timeVal < 6.5 {
		return 5 * time.Minute
	}
	return 35 * time.Second
}

func scanActiveAttendance(manual bool) string {
	state.mu.Lock()
	state.LastScanTime = time.Now()
	state.mu.Unlock()

	courses := state.PrimaryClient.GetCourses()
	if len(courses) == 0 {
		_ = state.PrimaryClient.UpdateCache(false)
		courses = state.PrimaryClient.GetCourses()
	}

	if len(courses) == 0 {
		return "Data mata kuliah belum tersedia."
	}

	var results []string
	foundOpen := 0

	for _, c := range courses {
		key, err := state.PrimaryClient.CheckActivePresence(c.Nomor, c.JenisSchema)
		if err != nil || key == "" {
			continue
		}

		foundOpen++
		state.mu.RLock()
		alreadyAttended := state.AttendedKeys[key]
		state.mu.RUnlock()

		cName := c.CourseName()
		if alreadyAttended {
			results = append(results, fmt.Sprintf("• %s: Presensi telah tercatat.", cName))
			continue
		}

		ok, pesan, err := state.PrimaryClient.SubmitAttendance(c.Nomor, c.JenisSchema, key, c.KuliahAsal)
		if err == nil && ok {
			state.mu.Lock()
			state.AttendedKeys[key] = true
			state.mu.Unlock()
			saveAttendedKeys()

			results = append(results, fmt.Sprintf("• %s: Berhasil diabsenkan (%s)", cName, pesan))
			appendFileLog(fmt.Sprintf("[INFO] PRESENSI DITEMUKAN & BERHASIL: %s | Key: %s (%s)", cName, key, pesan))
			notifyAttendanceSuccess(cName, c.Dosen, key, pesan)
		} else {
			results = append(results, fmt.Sprintf("• %s: Gagal submit (%s)", cName, pesan))
			appendFileLog(fmt.Sprintf("[WARNING] PRESENSI GAGAL: %s | Key: %s (%s)", cName, key, pesan))
		}
	}

	nowStr := getWIBStr()
	if manual {
		if foundOpen == 0 {
			return fmt.Sprintf("<b>HASIL PEMINDAIAN PRESENSI</b>\n"+
				"<code>Waktu: %s</code>\n\n"+
				"Tidak ada presensi yang sedang dibuka dosen pada %d mata kuliah terdaftar.", nowStr, len(courses))
		}
		return fmt.Sprintf("<b>HASIL PEMINDAIAN PRESENSI:</b>\n\n%s", strings.Join(results, "\n"))
	}

	return strings.Join(results, "\n")
}

func notifyAttendanceSuccess(course, dosen, key, pesan string) {
	if state.TgBot == nil || state.Creds.TelegramChatID == "" {
		return
	}
	nowStr := getWIBStr()
	msg := fmt.Sprintf("<b>PRESENSI BERHASIL TERCATAT</b>\n"+
		"<code>"+
		"Mata Kuliah : %s\n"+
		"Dosen       : %s\n"+
		"Kode Key    : %s\n"+
		"Waktu       : %s\n"+
		"Status      : %s"+
		"</code>", course, dosen, key, nowStr, pesan)

	_, _ = state.TgBot.SendMessage(state.Creds.TelegramChatID, msg, nil)
}

// Formatters 100% Identical to Python V2
func getStatusBox() string {
	nowWib := getWIBNow()
	nowTimeStr := nowWib.Format("15:04")

	state.mu.RLock()
	userInfo := state.PrimaryClient.UserInfo
	isCooldown := isCooldownActiveToday()
	forceSiaga := state.ForceSiaga
	schedules := state.PrimaryClient.GetSchedules()
	state.mu.RUnlock()

	// 1. Server Terputus
	if userInfo == nil {
		return "<code>┌─ STATUS ────────────\n" +
			"│ 🔴 Server Terputus\n" +
			"│ ⚠️ Butuh /relogin\n" +
			"└─────────────────────</code>"
	}

	// 2. Cooldown Aktif
	if isCooldown {
		return "<code>┌─ STATUS ────────────\n" +
			"│ 🟡 Mode Cooldown\n" +
			"│ 💤 Jeda s/d 00:00 WIB\n" +
			"└─────────────────────</code>"
	}

	// 3. Kuliah Berlangsung
	dayNames := map[time.Weekday]string{
		time.Monday: "senin", time.Tuesday: "selasa", time.Wednesday: "rabu",
		time.Thursday: "kamis", time.Friday: "jumat", time.Saturday: "sabtu", time.Sunday: "minggu",
	}
	todayDay := dayNames[nowWib.Weekday()]

	for _, item := range schedules {
		h := strings.ToLower(strings.TrimSpace(item.Hari))
		if h == todayDay {
			jStart := item.JamAwal
			jEnd := item.JamAkhir
			if jStart != "" && jEnd != "" && jStart <= nowTimeStr && nowTimeStr <= jEnd {
				mkNama := item.CourseName()
				mkShort := mkNama
				if len(mkShort) > 15 {
					mkShort = mkShort[:13] + ".."
				}
				return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
					"│ 🔵 Kuliah: %s\n"+
					"│ ⚡ Siaga Presensi\n"+
					"└─────────────────────</code>", mkShort)
			}
		}
	}

	// 4. Force Siaga
	if forceSiaga {
		return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
			"│ 🟢 Siaga Penuh\n"+
			"│ 🕒 %s WIB (Override)\n"+
			"└─────────────────────</code>", nowTimeStr)
	}

	// 5. Waktu Malam & Subuh
	curHour := nowWib.Hour()
	curMin := nowWib.Minute()
	timeVal := float64(curHour) + float64(curMin)/60.0

	if curHour >= 21 || curHour < 4 {
		return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
			"│ 💤 Istirahat Malam\n"+
			"│ 🕒 %s (Standby)\n"+
			"└─────────────────────</code>", nowTimeStr)
	}
	if timeVal >= 4.0 && timeVal < 6.5 {
		return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
			"│ 🌅 Siaga Subuh\n"+
			"│ 🕒 %s (Standby)\n"+
			"└─────────────────────</code>", nowTimeStr)
	}

	// 6. Aktif Normal
	return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
		"│ 🟢 Aktif & Listening\n"+
		"│ 🕒 %s WIB (SSO OK)\n"+
		"└─────────────────────</code>", nowTimeStr)
}

func getWelcomeText(page int) string {
	box := getStatusBox()
	if page == 2 {
		return fmt.Sprintf("<b>KON-THOL ASSISTANT</b>\n"+
			"<i>Menu Sistem, Notifikasi & Log</i>\n\n"+
			"%s\n\n"+
			"✦ <b>Creator : Gungna</b>\n\n"+
			"Silakan pilih menu lanjutan di bawah:", box)
	}
	return fmt.Sprintf("<b>KON-THOL ASSISTANT</b>\n"+
		"<i>Kawan Otomasi dan Notifikasi E-THOL</i>\n\n"+
		"%s\n\n"+
		"✦ <b>Creator : Gungna</b>\n\n"+
		"Silakan pilih menu di bawah ini:", box)
}

func sendWelcomeMenu() {
	if state.TgBot == nil || state.Creds.TelegramChatID == "" {
		return
	}
	text := getWelcomeText(1)
	kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
	state.TgBot.SendMenu(state.Creds.TelegramChatID, text, kb, state.BannerPaths)
}

func formatStatusText() string {
	state.mu.RLock()
	userInfo := state.PrimaryClient.UserInfo
	isCooldown := isCooldownActiveToday()
	forceSiaga := state.ForceSiaga
	state.mu.RUnlock()

	nama := "Belum login"
	nrp := "-"
	if userInfo != nil {
		nama = userInfo.Nama
		nrp = userInfo.NipNrp
	}
	nowStr := getWIBStr()
	lastAuth := state.PrimaryClient.LastAuthTime.Format("02-01-2006 15:04:05 WIB")
	if state.PrimaryClient.LastAuthTime.IsZero() {
		lastAuth = nowStr
	}

	nowWib := getWIBNow()
	timeVal := float64(nowWib.Hour()) + float64(nowWib.Minute())/60.0

	var scannerStatus, scannerSub, jadwalRelogin, aktivitas string

	if isCooldown {
		scannerStatus = fmt.Sprintf("🟡 Cooldown (%s)", state.CooldownDate)
		scannerSub = "💤 Jeda s/d 00:00 WIB"
		jadwalRelogin = "Auto re-login esok hari (00:00 WIB)"
		aktivitas = "Istirahat (monitoring agresif jeda)"
	} else if forceSiaga {
		scannerStatus = "🟢 Siaga Penuh (Override)"
		scannerSub = "• Mode Malam di-bypass (Siaga Agresif)"
		jadwalRelogin = "Pengecekan sesi & re-login tiap 10 menit"
		aktivitas = "Siaga penuh memantau perkuliahan malam"
	} else if timeVal >= 21.5 || timeVal < 4.0 {
		scannerStatus = "💤 Istirahat Malam"
		scannerSub = "• Jeda malam (dosen offline)"
		jadwalRelogin = "Siaga subuh (04:00 WIB)"
		aktivitas = "Standby malam (gunakan /resume jika ada kuliah)"
	} else if timeVal >= 4.0 && timeVal < 6.5 {
		scannerStatus = "🌅 Siaga Subuh"
		scannerSub = "• Memantau persiapan kuliah pagi"
		jadwalRelogin = "Pengecekan sesi & re-login tiap 10 menit"
		aktivitas = "Siaga subuh menyambut jadwal kuliah"
	} else {
		scannerStatus = "🟢 Siaga Penuh"
		scannerSub = "• Siaga Presensi Perkuliahan"
		jadwalRelogin = "Pengecekan sesi & re-login tiap 10 menit"
		aktivitas = "Siaga memantau presensi & jadwal"
	}

	return fmt.Sprintf("<b>┌─ DATA MAHASISWA ─────────────────</b>\n"+
		"│ Mahasiswa      : %s\n"+
		"│ NRP            : <code>%s</code>\n"+
		"│ Waktu Server   : %s\n"+
		"<b>├─ SESI LOGIN & RE-LOGIN ───────────</b>\n"+
		"│ Sesi Login     : 🟢 Terhubung (Aktif)\n"+
		"│ Terakhir Login : <code>%s</code>\n"+
		"│ Jadwal Re-login: %s\n"+
		"<b>├─ OPERASIONAL SCANNER ────────────</b>\n"+
		"│ Status Scanner : %s\n"+
		"│                  %s\n"+
		"│ Aktivitas      : %s\n"+
		"<b>└──────────────────────────────────</b>",
		nama, nrp, nowStr, lastAuth, jadwalRelogin, scannerStatus, scannerSub, aktivitas)
}

func formatJadwalText() string {
	_ = state.PrimaryClient.UpdateCache(true)
	schedules := state.PrimaryClient.GetSchedules()
	if len(schedules) == 0 {
		return "Data jadwal perkuliahan belum tersedia."
	}

	nowWib := getWIBNow()
	dayMap := map[time.Weekday]string{
		time.Monday: "senin", time.Tuesday: "selasa", time.Wednesday: "rabu",
		time.Thursday: "kamis", time.Friday: "jumat", time.Saturday: "sabtu", time.Sunday: "minggu",
	}
	todayDayClean := dayMap[nowWib.Weekday()]

	cleanDay := func(d string) string {
		return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(d, "'", ""), "`", "")))
	}

	dayOrder := map[string]int{
		"senin": 1, "selasa": 2, "rabu": 3, "kamis": 4, "jumat": 5, "sabtu": 6, "minggu": 7,
	}

	sortedJadwal := make([]client.ScheduleItem, len(schedules))
	copy(sortedJadwal, schedules)

	sort.Slice(sortedJadwal, func(i, j int) bool {
		valI := sortedJadwal[i].NomorHari
		if valI <= 0 {
			valI = dayOrder[cleanDay(sortedJadwal[i].Hari)]
			if valI == 0 {
				valI = 99
			}
		}
		valJ := sortedJadwal[j].NomorHari
		if valJ <= 0 {
			valJ = dayOrder[cleanDay(sortedJadwal[j].Hari)]
			if valJ == 0 {
				valJ = 99
			}
		}
		if valI != valJ {
			return valI < valJ
		}
		return sortedJadwal[i].JamAwal < sortedJadwal[j].JamAwal
	})

	tahun := time.Now().Year()
	sem := 1
	if state.PrimaryClient.EtholConfig != nil {
		tahun = state.PrimaryClient.EtholConfig.TahunAktif
		sem = state.PrimaryClient.EtholConfig.SemesterAktif
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>JADWAL KULIAH (Semester %d/%d):</b>\n", sem, tahun))

	currDay := ""
	for _, item := range sortedJadwal {
		dRaw := strings.TrimSpace(item.Hari)
		if dRaw == "" || strings.EqualFold(dRaw, "none") {
			dRaw = "Lainnya"
		}
		dClean := cleanDay(dRaw)

		if dRaw != currDay {
			currDay = dRaw
			if dClean == todayDayClean {
				sb.WriteString(fmt.Sprintf("\n🗓️ <b>[%s (HARI INI)]</b>\n", strings.ToUpper(currDay)))
			} else {
				sb.WriteString(fmt.Sprintf("\n🗓️ <b>[%s]</b>\n", strings.ToUpper(currDay)))
			}
		}

		jamAwal := item.JamAwal
		jamAkhir := item.JamAkhir
		jamStr := fmt.Sprintf("%s - %s", jamAwal, jamAkhir)
		if jamAwal == "" || strings.EqualFold(jamAwal, "none") || jamAwal == "-" {
			jamStr = "Fleksibel / Mandiri"
		}

		mk := item.CourseName()
		ruang := item.Ruang
		if ruang == "" {
			ruang = "Online"
		}

		sb.WriteString(fmt.Sprintf("• <b>%s</b>\n", mk))
		sb.WriteString(fmt.Sprintf("  ⏰ <code>%s</code> • 📍 %s\n", jamStr, ruang))
	}

	return sb.String()
}

func formatRekapDetail() string {
	todayStr := getWIBNow().Format("02-01-2006")
	stats, err := state.PrimaryClient.GetAttendanceStatistics(todayStr)
	if err != nil || stats == nil {
		return "Gagal memuat rekapitulasi kehadiran dari server ETHOL."
	}

	nowWib := getWIBNow()
	dayMap := map[time.Weekday]string{
		time.Monday: "senin", time.Tuesday: "selasa", time.Wednesday: "rabu",
		time.Thursday: "kamis", time.Friday: "jumat", time.Saturday: "sabtu", time.Sunday: "minggu",
	}
	todayDayClean := dayMap[nowWib.Weekday()]

	cleanDay := func(d string) string {
		return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(d, "'", ""), "`", "")))
	}

	schedules := state.PrimaryClient.GetSchedules()
	coursesScheduledToday := make(map[string]bool)
	courseScheduledDays := make(map[string]string)

	for _, s := range schedules {
		hClean := cleanDay(s.Hari)
		mkName := s.CourseName()
		if hClean != "" && mkName != "" {
			courseScheduledDays[mkName] = s.Hari
		}
		if hClean == todayDayClean {
			coursesScheduledToday[mkName] = true
			coursesScheduledToday[fmt.Sprintf("%d", s.Kuliah)] = true
		}
	}

	var sb strings.Builder
	sb.WriteString("<b>REKAPITULASI KEHADIRAN RESMI</b>\n\n")
	sb.WriteString(fmt.Sprintf("• Rata-rata Total : <b>%.1f%%</b>\n", stats.Percentage))
	sb.WriteString(fmt.Sprintf("• Total Kehadiran : %d dari %d sesi perkuliahan\n", stats.TotalMhsSemester, stats.TotalDosenSemester))
	sb.WriteString(fmt.Sprintf("• Hadir Hari Ini  : %d sesi tervalidasi hadir\n\n", stats.TotalMhsToday))
	sb.WriteString("<b>RINCIAN PER MATA KULIAH:</b>\n\n")

	for _, item := range stats.Breakdown {
		mkName := item.Nama
		kIDStr := fmt.Sprintf("%d", item.KuliahID)
		isToday := coursesScheduledToday[mkName] || coursesScheduledToday[kIDStr]

		if isToday {
			statusSesi := "⚪ <code>[Belum Ada Sesi Dibuka Dosen]</code>"
			if item.MToday > 0 {
				statusSesi = fmt.Sprintf("🟢 <code>[Sesi Selesai: Tervalidasi Hadir (%d Sesi)]</code>", item.MToday)
			} else if item.DToday > 0 {
				statusSesi = "⚠️ <code>[Sesi Terbuka: Belum Hadir]</code>"
			}

			sb.WriteString(fmt.Sprintf("• <b>%s</b> (Hari Ini)\n", mkName))
			sb.WriteString(fmt.Sprintf("  Status Sesi : %s\n", statusSesi))
			sb.WriteString(fmt.Sprintf("  Total Hadir : %d kali pertemuan dalam semester ini.\n\n", item.Hadir))
		} else if item.MToday > 0 || item.DToday > 0 {
			jadwalAsli := courseScheduledDays[mkName]
			if jadwalAsli == "" {
				jadwalAsli = "Hari Lain"
			}
			statusSesi := fmt.Sprintf("🟠 <code>[Sesi Luar Jadwal: Sesi Terbuka (%s)]</code>", jadwalAsli)
			if item.MToday > 0 {
				statusSesi = fmt.Sprintf("🟠 <code>[Sesi Luar Jadwal: Tervalidasi Hadir (%d Sesi • Jadwal: %s)]</code>", item.MToday, jadwalAsli)
			}

			sb.WriteString(fmt.Sprintf("• <b>%s</b> (Luar Hari)\n", mkName))
			sb.WriteString(fmt.Sprintf("  Status Sesi : %s\n", statusSesi))
			sb.WriteString(fmt.Sprintf("  Total Hadir : %d kali pertemuan dalam semester ini.\n\n", item.Hadir))
		} else {
			sb.WriteString(fmt.Sprintf("• <b>%s</b>\n", mkName))
			sb.WriteString(fmt.Sprintf("  Total Hadir : %d kali pertemuan dalam semester ini.\n\n", item.Hadir))
		}
	}

	return sb.String()
}

func formatTugasText() string {
	tasks, err := state.PrimaryClient.GetPendingTasks()
	if err != nil || len(tasks) == 0 {
		return "<b>DAFTAR TUGAS KULIAH</b>\n\n" +
			"Semua tugas pada semester ini telah dikumpulkan atau tidak ada tugas aktif."
	}

	var sb strings.Builder
	sb.WriteString("<b>DAFTAR TUGAS PENDING (BELUM DIKUMPULKAN):</b>\n\n")

	linksDict := make(map[string]string)
	for idx, t := range tasks {
		if t.KuliahID > 0 && linksDict[t.Matkul] == "" {
			linksDict[t.Matkul] = fmt.Sprintf("https://ethol.pens.ac.id/mahasiswa/matakuliah/%d/tugas", t.KuliahID)
		}
		sb.WriteString(fmt.Sprintf("<b>%d. %s</b>\n", idx+1, t.Title))
		sb.WriteString(fmt.Sprintf("   Mata Kuliah : %s\n", t.Matkul))
		sb.WriteString(fmt.Sprintf("   Tenggat     : <code>%s</code>\n\n", t.Deadline))
	}

	if len(linksDict) == 1 {
		for _, urlTugas := range linksDict {
			sb.WriteString(fmt.Sprintf("Tautan Web : %s\n", urlTugas))
		}
	} else {
		sb.WriteString("<b>Tautan Web Pengumpulan:</b>\n")
		for mkName, urlTugas := range linksDict {
			sb.WriteString(fmt.Sprintf("• %s :\n  %s\n", mkName, urlTugas))
		}
	}

	sb.WriteString("\n⚠️ <i>Catatan: Harap pastikan Anda sudah login ke akun ETHOL di browser terlebih dahulu sebelum membuka tautan di atas agar dapat langsung diarahkan ke tugas tersebut.</i>")
	return sb.String()
}

func formatHelpText() string {
	return "<b>PANDUAN PERINTAH LENGKAP KON-THOL:</b>\n\n" +
		"⚡ <b>Presensi & Akademik:</b>\n" +
		"• /scan atau /absen - Periksa & isi presensi seketika\n" +
		"• /jadwal atau /matkul - Jadwal mingguan + status sesi hari ini\n" +
		"• /tugas - Daftar tugas pending & link pengumpulan\n" +
		"• /rekap - Rekapitulasi persentase kehadiran per matkul\n\n" +
		"🔧 <b>Sistem & Pengaturan:</b>\n" +
		"• /status - Informasi akun & status engine\n" +
		"• /notif - Cek 5 notifikasi terakhir di portal ETHOL\n" +
		"• /log - Riwayat log aktivitas berfilter\n" +
		"• /cooldown - Istirahatkan bot setelah kelas hari ini usai\n" +
		"• /resume - Batalkan cooldown & kembali siaga\n" +
		"• /relogin - Sinkronisasi ulang sesi login SSO PENS\n" +
		"• /public - Akses integrasi Multi-Account Public Edition\n" +
		"• /help - Tampilkan panduan ini\n\n" +
		"👨‍💻 <i>Credit : Gungna</i>"
}

func getRecentNotifs() string {
	_, lines, err := state.PrimaryClient.GetUnreadNotifications()
	if err == nil && len(lines) > 0 {
		return fmt.Sprintf("🔔 <b>5 NOTIFIKASI TERAKHIR DI ETHOL:</b>\n\n%s", strings.Join(lines[:min(5, len(lines))], "\n\n"))
	}
	return "Belum ada riwayat notifikasi di portal ETHOL."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getFilteredLogs(filterType string) string {
	data, err := os.ReadFile(state.LogPath)
	if err != nil {
		return "File log belum tersedia."
	}

	allLines := strings.Split(string(data), "\n")
	var validLines []string
	for _, l := range allLines {
		t := strings.TrimSpace(l)
		if t != "" {
			validLines = append(validLines, t)
		}
	}

	switch filterType {
	case "error":
		errKeywords := []string{"[error]", "[warning]", "gagal", "exception"}
		var matched []string
		for _, l := range validLines {
			low := strings.ToLower(l)
			for _, k := range errKeywords {
				if strings.Contains(low, k) {
					matched = append(matched, l)
					break
				}
			}
		}
		if len(matched) == 0 {
			return "⚠️ <b>LOG AKTIVITAS [ERROR & PERINGATAN] (WIB):</b>\n\n" +
				"<i>Sistem berjalan normal. Tidak ada catatan error atau peringatan pada log.</i>"
		}
		start := 0
		if len(matched) > 12 {
			start = len(matched) - 12
		}
		var escaped []string
		for _, m := range matched[start:] {
			escaped = append(escaped, html.EscapeString(m))
		}
		return "⚠️ <b>LOG AKTIVITAS [ERROR & PERINGATAN] (WIB):</b>\n\n<code>" + strings.Join(escaped, "\n") + "</code>"

	case "auth":
		authKeywords := []string{"login", "cas", "sso", "listener", "validasi-token", "scanner daemon", "otentikasi", "refresh"}
		var matched []string
		for _, l := range validLines {
			low := strings.ToLower(l)
			for _, k := range authKeywords {
				if strings.Contains(low, k) {
					matched = append(matched, l)
					break
				}
			}
		}
		if len(matched) == 0 {
			return "🔑 <b>LOG LOGIN & LISTENER:</b>\n\n<i>Belum ada log sesi tercatat.</i>"
		}
		start := 0
		if len(matched) > 12 {
			start = len(matched) - 12
		}
		var escaped []string
		for _, m := range matched[start:] {
			escaped = append(escaped, html.EscapeString(m))
		}
		return "🔑 <b>LOG AKTIVITAS [LOGIN & LISTENER] (WIB):</b>\n\n<code>" + strings.Join(escaped, "\n") + "</code>"

	case "notif":
		notifKeywords := []string{"notifikasi", "notif", "tugas-baru", "presensi-kuliah"}
		var matched []string
		for _, l := range validLines {
			low := strings.ToLower(l)
			for _, k := range notifKeywords {
				if strings.Contains(low, k) {
					matched = append(matched, l)
					break
				}
			}
		}
		if len(matched) == 0 {
			return "🔔 <b>LOG NOTIFIKASI MASUK:</b>\n\n<i>Belum ada notifikasi baru yang tercatat.</i>"
		}
		start := 0
		if len(matched) > 12 {
			start = len(matched) - 12
		}
		var escaped []string
		for _, m := range matched[start:] {
			escaped = append(escaped, html.EscapeString(m))
		}
		return "🔔 <b>LOG AKTIVITAS [NOTIFIKASI] (WIB):</b>\n\n<code>" + strings.Join(escaped, "\n") + "</code>"

	case "presensi":
		presKeywords := []string{"presensi", "berhasil", "absen", "submit", "attended"}
		var matched []string
		for _, l := range validLines {
			low := strings.ToLower(l)
			for _, k := range presKeywords {
				if strings.Contains(low, k) {
					matched = append(matched, l)
					break
				}
			}
		}
		if len(matched) == 0 {
			return "✅ <b>LOG PRESENSI BERHASIL:</b>\n\n<i>Belum ada presensi yang tercatat hari ini.</i>"
		}
		start := 0
		if len(matched) > 12 {
			start = len(matched) - 12
		}
		var escaped []string
		for _, m := range matched[start:] {
			escaped = append(escaped, html.EscapeString(m))
		}
		return "✅ <b>LOG AKTIVITAS [PRESENSI BERHASIL] (WIB):</b>\n\n<code>" + strings.Join(escaped, "\n") + "</code>"

	default: // all
		start := 0
		if len(validLines) > 12 {
			start = len(validLines) - 12
		}
		var escaped []string
		for _, l := range validLines[start:] {
			escaped = append(escaped, html.EscapeString(l))
		}
		return "📜 <b>LOG AKTIVITAS SISTEM [GLOBAL] (WIB):</b>\n\n<code>" + strings.Join(escaped, "\n") + "</code>"
	}
}

func formatKontholPublicCard() string {
	accCount := 0
	waTarget := "Belum diatur"
	waProvider := "Belum diatur"

	hasAcc := false
	if state.Accounts != nil && len(state.Accounts.Accounts) > 0 {
		hasAcc = true
		accCount = len(state.Accounts.Accounts)
		if state.Accounts.WhatsApp.TargetPhone != "" {
			waTarget = state.Accounts.WhatsApp.TargetPhone
		}
		if state.Accounts.WhatsApp.Provider != "" {
			waProvider = strings.ToUpper(state.Accounts.WhatsApp.Provider)
		} else {
			waProvider = "FONNTE"
		}
	}

	statusFile := "🟡 Belum ada <code>accounts.json</code> (Menggunakan template contoh)"
	if hasAcc {
		statusFile = "🟢 <code>accounts.json</code> (Aktif)"
	}

	return fmt.Sprintf("👥 <b>KON-THOL PUBLIC EDITION — INTEGRATION</b>\n"+
		"<i>Multi-Account Concurrency & WhatsApp Gateway Engine</i>\n\n"+
		"<b>┌─ STATUS MODUL PUBLIK ────────────</b>\n"+
		"│ File Config    : %s\n"+
		"│ Akun Mahasiswa : <b>%d</b>\n"+
		"│ WA Gateway     : <b>%s</b>\n"+
		"│ Nomor Target   : <code>%s</code>\n"+
		"│ Mode Eksekusi  : Multi-threaded (ThreadPool)\n"+
		"│ Sesi Presensi  : Terisolasi per Akun\n"+
		"<b>└──────────────────────────────────</b>\n\n"+
		"💡 <i>Gunakan tombol di bawah untuk memicu pemindaian serentak seluruh akun mahasiswa atau mengecek detail daftar akun.</i>",
		statusFile, accCount, waProvider, waTarget)
}

func formatPublicAccountsList() string {
	if state.Accounts == nil || len(state.Accounts.Accounts) == 0 {
		return "📋 <b>DAFTAR AKUN MAHASISWA</b>\n\nBelum ada akun yang terdaftar dalam konfigurasi."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 <b>DAFTAR AKUN MAHASISWA (%d AKUN):</b>\n\n", len(state.Accounts.Accounts)))

	for idx, acc := range state.Accounts.Accounts {
		name := acc.Name
		if name == "" {
			name = fmt.Sprintf("Mahasiswa %d", idx+1)
		}
		user := acc.Username
		maskedUser := user
		if strings.Contains(user, "@") {
			parts := strings.SplitN(user, "@", 2)
			if len(parts[0]) > 3 {
				maskedUser = parts[0][:3] + "***@" + parts[1]
			}
		} else if len(user) > 3 {
			maskedUser = user[:3] + "***"
		}
		wa := acc.WATarget
		if wa == "" {
			wa = "-"
		}

		sb.WriteString(fmt.Sprintf("<b>%d. %s</b>\n   • Akun : <code>%s</code>\n   • WA   : <code>%s</code>\n\n",
			idx+1, name, maskedUser, wa))
	}

	return sb.String()
}

// Commands & Action Handlers
func handleTgCommand(cmd string, chatID int64, msgID int) {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	adminChatID, _ := strconv.ParseInt(state.Creds.TelegramChatID, 10, 64)

	if adminChatID != 0 && chatID != adminChatID {
		_, _ = state.TgBot.SendMessage(chatID, "Akses ditolak. Bot ini dikhususkan untuk akun terdaftar.", nil)
		return
	}

	appendFileLog(fmt.Sprintf("[INFO] Command diterima: %s", cmd))

	switch {
	case contains([]string{"/start", "/help", "help", "/menu", "menu"}, cmdLower):
		sendWelcomeMenu()

	case contains([]string{"/status", "status"}, cmdLower):
		txt := formatStatusText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetStatusKeyboard())

	case contains([]string{"/rekap", "rekap"}, cmdLower):
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang menghitung rekapitulasi kehadiran per mata kuliah...</i>", telegram.GetBackKeyboard())
		txt := formatRekapDetail()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case contains([]string{"/cooldown", "/istirahat", "cooldown", "istirahat"}, cmdLower):
		state.mu.Lock()
		state.CooldownActive = true
		state.CooldownDate = getWIBNow().Format("2006-01-02")
		state.mu.Unlock()
		appendFileLog("[INFO] Mode cooldown diaktifkan")
		sendWelcomeMenu()

	case contains([]string{"/public", "public", "/multi", "multi"}, cmdLower):
		txt := formatKontholPublicCard()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetKontholPublicKeyboard())

	case contains([]string{"/resume", "/siaga", "/batalistirahat", "resume", "siaga"}, cmdLower):
		state.mu.Lock()
		state.CooldownActive = false
		state.ForceSiaga = true
		state.mu.Unlock()
		appendFileLog("[INFO] Mode siaga penuh diaktifkan")
		sendWelcomeMenu()

	case contains([]string{"/scan", "/absen", "scan", "absen"}, cmdLower):
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang memindai seluruh mata kuliah ke server ETHOL...</i>", telegram.GetBackKeyboard())
		txt := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case contains([]string{"/tugas", "tugas"}, cmdLower):
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang mengambil data tugas perkuliahan...</i>", telegram.GetBackKeyboard())
		txt := formatTugasText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case contains([]string{"/jadwal", "/matkul", "jadwal", "matkul"}, cmdLower):
		txt := formatJadwalText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case contains([]string{"/notif", "/notifikasi", "notif"}, cmdLower):
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Mengambil notifikasi portal ETHOL...</i>", telegram.GetPage2BackKeyboard())
		txt := getRecentNotifs()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPage2BackKeyboard())

	case contains([]string{"/log", "/logs", "log"}, cmdLower):
		txt := getFilteredLogs("all")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case contains([]string{"/relogin", "relogin"}, cmdLower):
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang merefresh otentikasi login ETHOL...</i>", telegram.GetPage2BackKeyboard())
		ok, err := state.PrimaryClient.LoginCAS()
		if ok {
			_ = state.PrimaryClient.UpdateCache(true)
			appendFileLog("[INFO] Sesi login disegarkan ulang")
			state.TgBot.ShowContentCard(chatID, "✅ <b>SESI DIPERBARUI</b>\nOtentikasi login dan sinkronisasi data kuliah berhasil disegarkan.", telegram.GetPage2BackKeyboard())
		} else {
			state.TgBot.ShowContentCard(chatID, fmt.Sprintf("❌ <b>GAGAL REFRESH SESI</b>\n%v", err), telegram.GetPage2BackKeyboard())
		}

	default:
		sendWelcomeMenu()
	}
}

func contains(arr []string, target string) bool {
	for _, a := range arr {
		if a == target {
			return true
		}
	}
	return false
}

func handleTgAction(action string, chatID int64, msgID int, queryID string) {
	switch action {
	case "btn_menu", "btn_page_1":
		sendWelcomeMenu()

	case "btn_page_2":
		text := getWelcomeText(2)
		kb := telegram.GetMainKeyboard(2, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "btn_status":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang memuat status autentikasi SSO...</i>", telegram.GetStatusKeyboard())
		txt := formatStatusText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetStatusKeyboard())

	case "btn_scan":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang memindai seluruh mata kuliah ke server ETHOL...</i>", telegram.GetBackKeyboard())
		txt := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
		if !strings.Contains(txt, "Gagal") && !strings.Contains(txt, "Error") && !strings.Contains(txt, "belum tersedia") {
			go func() {
				time.Sleep(3 * time.Second)
				sendWelcomeMenu()
			}()
		}

	case "btn_rekap":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang menghitung rekapitulasi kehadiran per mata kuliah...</i>", telegram.GetBackKeyboard())
		txt := formatRekapDetail()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_tugas":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang mengambil data tugas perkuliahan...</i>", telegram.GetBackKeyboard())
		txt := formatTugasText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_jadwal":
		txt := formatJadwalText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_notif":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Mengambil notifikasi portal ETHOL...</i>", telegram.GetPage2BackKeyboard())
		txt := getRecentNotifs()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPage2BackKeyboard())

	case "btn_log", "btn_log_all":
		txt := getFilteredLogs("all")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_err":
		txt := getFilteredLogs("error")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_auth":
		txt := getFilteredLogs("auth")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_notif":
		txt := getFilteredLogs("notif")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_pres":
		txt := getFilteredLogs("presensi")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_help":
		txt := formatHelpText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPage2BackKeyboard())

	case "btn_relogin":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang merefresh otentikasi login ETHOL...</i>", telegram.GetPage2BackKeyboard())
		ok, err := state.PrimaryClient.LoginCAS()
		if ok {
			_ = state.PrimaryClient.UpdateCache(true)
			appendFileLog("[INFO] Sesi login disegarkan ulang")
			state.TgBot.ShowContentCard(chatID, "✅ <b>SESI DIPERBARUI</b>\nOtentikasi login dan sinkronisasi data kuliah berhasil disegarkan.", telegram.GetPage2BackKeyboard())
			go func() {
				time.Sleep(3 * time.Second)
				text := getWelcomeText(2)
				kb := telegram.GetMainKeyboard(2, isCooldownActiveToday())
				state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)
			}()
		} else {
			state.TgBot.ShowContentCard(chatID, fmt.Sprintf("❌ <b>GAGAL REFRESH SESI</b>\n%v", err), telegram.GetPage2BackKeyboard())
		}

	case "btn_cooldown":
		state.mu.Lock()
		state.CooldownActive = true
		state.CooldownDate = getWIBNow().Format("2006-01-02")
		state.mu.Unlock()
		appendFileLog("[INFO] Mode cooldown diaktifkan")
		state.TgBot.ShowContentCard(chatID, "🟡 <b>MODE COOLDOWN DIAKTIFKAN</b>\nPolling agresif diistirahatkan hingga tengah malam (00:00 WIB).", telegram.GetBackKeyboard())
		go func() {
			time.Sleep(3 * time.Second)
			sendWelcomeMenu()
		}()

	case "btn_resume":
		state.mu.Lock()
		state.CooldownActive = false
		state.ForceSiaga = true
		state.mu.Unlock()
		appendFileLog("[INFO] Mode siaga penuh diaktifkan")
		state.TgBot.ShowContentCard(chatID, "🟢 <b>SIAGA PENUH DIAKTIFKAN</b>\nBot kembali memantau presensi dan jadwal secara aktif.", telegram.GetBackKeyboard())
		go func() {
			time.Sleep(3 * time.Second)
			sendWelcomeMenu()
		}()

	case "btn_konthol_public":
		txt := formatKontholPublicCard()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetKontholPublicKeyboard())

	case "btn_public_scan":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang menjalankan siklus scan serentak seluruh akun mahasiswa...</i>", telegram.GetKontholPublicKeyboard())
		txt := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetKontholPublicKeyboard())

	case "btn_public_accounts":
		txt := formatPublicAccountsList()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetKontholPublicKeyboard())
	}
}
