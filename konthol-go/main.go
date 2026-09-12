package main

import (
	"encoding/json"
	"flag"
	"fmt"
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

type BotState struct {
	CredPath       string
	AccPath        string
	StatePath      string
	BannerPaths    []string
	Creds          *config.Credentials
	Accounts       *config.AccountsConfig
	PrimaryClient  *client.EtholClient
	TgBot          *telegram.TelegramBot
	CooldownActive bool
	CooldownDate   string
	ForceSiaga     bool
	AttendedKeys   map[string]bool
	ActivityLogs   []string
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

	// Load Accounts (Opsional)
	if accs, err := config.LoadAccounts(state.AccPath); err == nil {
		state.Accounts = accs
		log.Printf("[CONFIG] Multi-akun terdeteksi: %d akun mahasiswa", len(accs.Accounts))
	}

	// 2. Init Ethol Client
	etholCl, err := client.NewEtholClient(creds.Username, creds.Password)
	if err != nil {
		log.Fatalf("[FATAL] Inisialisasi client ETHOL gagal: %v", err)
	}
	state.PrimaryClient = etholCl

	// 3. Login CAS SSO
	ok, err := etholCl.LoginCAS()
	if !ok || err != nil {
		log.Printf("[WARN] Login CAS awal terkendala: %v (Akan dicoba ulang)", err)
		addLog(fmt.Sprintf("Login CAS terkendala: %v", err))
	} else {
		addLog("Login CAS SSO berhasil")
		if err := etholCl.UpdateCache(true); err != nil {
			log.Printf("[WARN] Inisialisasi cache gagal: %v", err)
		}
	}

	// 4. Init Telegram Bot
	if creds.TelegramToken != "" {
		tg := telegram.NewTelegramBot(creds.TelegramToken, creds.TelegramChatID)
		state.TgBot = tg
		tg.CommandHandler = handleTgCommand
		tg.ActionHandler = handleTgAction

		stopChan := make(chan struct{})
		go tg.StartPolling(stopChan)

		// Kirim tampilan menu utama awal
		sendWelcomeMenu()
	}

	// 5. Start Polling Engine Goroutine
	stopLoop := make(chan struct{})
	go runSchedulerLoop(stopLoop)

	log.Println("[SYSTEM] KON-THOL aktif penuh. Menunggu sinyal interrupt...")

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

func addLog(msg string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	entry := fmt.Sprintf("[%s] %s", getWIBNow().Format("15:04:05"), msg)
	state.ActivityLogs = append(state.ActivityLogs, entry)
	if len(state.ActivityLogs) > 20 {
		state.ActivityLogs = state.ActivityLogs[len(state.ActivityLogs)-20:]
	}
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
		log.Printf("[STATE] %d key presensi tersimpan dimuat", len(keys))
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
			state.mu.RUnlock()

			if isCooldown {
				continue
			}

			scanActiveAttendance(false)
		}
	}
}

func isCooldownActiveToday() bool {
	if !state.CooldownActive {
		return false
	}
	today := getWIBNow().Format("2006-01-02")
	return state.CooldownDate == today
}

func getNextInterval() time.Duration {
	now := getWIBNow()
	hour := now.Hour()
	minute := now.Minute()
	timeVal := float64(hour) + float64(minute)/60.0

	// 21:00 - 04:00 : Istirahat Malam (15 menit)
	if hour >= 21 || hour < 4 {
		return 15 * time.Minute
	}
	// 04:00 - 06:30 : Siaga Subuh (5 menit)
	if timeVal >= 4.0 && timeVal < 6.5 {
		return 5 * time.Minute
	}
	// Siaga Penuh Kuliah (35 detik)
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
			addLog(fmt.Sprintf("PRESENSI SUKSES: %s (%s)", cName, key))
			notifyAttendanceSuccess(cName, c.Dosen, key, pesan)
		} else {
			results = append(results, fmt.Sprintf("• %s: Gagal submit (%s)", cName, pesan))
			addLog(fmt.Sprintf("PRESENSI GAGAL: %s (%s)", cName, pesan))
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

// UI Formatters 100% Identical to Python V2
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

func getWelcomeText() string {
	box := getStatusBox()
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
	text := getWelcomeText()
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
		activeClean := "Tidak ada kelas"
		scannerSub = fmt.Sprintf("• %s", activeClean)
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

	sb.WriteString("\n⚠️ <i>Catatan: Harap pastikan sudah login ke akun ETHOL di browser terlebih dahulu sebelum membuka tautan di atas agar dapat langsung diarahkan ke tugas tersebut.</i>")
	return sb.String()
}

func formatHelpText() string {
	return "<b>PANDUAN PENGGUNAAN KON-THOL</b>\n\n" +
		"• <b>⚡ Presensi Manual</b>: Memindai portal seketika untuk mengecek & submit kode presensi aktif.\n" +
		"• <b>📅 Jadwal Kuliah</b>: Melihat jadwal perkuliahan terdaftar per hari.\n" +
		"• <b>📝 Tugas Pending</b>: Memeriksa tugas kuliah yang belum diserahkan.\n" +
		"• <b>📊 Rekap Kehadiran</b>: Menghitung persentase dan riwayat presensi resmi.\n" +
		"• <b>💤 Mode Cooldown</b>: Menjeda polling agresif hingga tengah malam.\n" +
		"• <b>🔄 Re-login Session</b>: Memperbarui otentikasi login CAS SSO PENS."
}

func formatLogsText() string {
	state.mu.RLock()
	logs := state.ActivityLogs
	state.mu.RUnlock()

	if len(logs) == 0 {
		return "<b>LOG AKTIVITAS SISTEM</b>\n\nBelum ada catatan aktivitas baru."
	}
	return fmt.Sprintf("<b>LOG AKTIVITAS SISTEM (%d Terakhir)</b>\n\n<code>%s</code>",
		len(logs), strings.Join(logs, "\n"))
}

func formatPublicAccounts() string {
	if state.Accounts == nil || len(state.Accounts.Accounts) == 0 {
		return "<b>DAFTAR AKUN TERDAFTAR</b>\n\nTidak ada multi-akun pada konfigurasi."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>DAFTAR AKUN MAHASISWA (%d)</b>\n\n", len(state.Accounts.Accounts)))
	for i, a := range state.Accounts.Accounts {
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n   NRP/User: <code>%s</code>\n   WA: <code>%s</code>\n\n",
			i+1, a.Name, a.Username, a.WATarget))
	}
	return sb.String()
}

// Telegram Command & Action Handlers
func handleTgCommand(cmd string, chatID int64, msgID int) {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	adminChatID, _ := strconv.ParseInt(state.Creds.TelegramChatID, 10, 64)

	if adminChatID != 0 && chatID != adminChatID {
		_, _ = state.TgBot.SendMessage(chatID, "Akses ditolak. Bot ini dikhususkan untuk akun terdaftar.", nil)
		return
	}

	switch {
	case cmdLower == "/start" || cmdLower == "/menu" || cmdLower == "help" || cmdLower == "/help":
		sendWelcomeMenu()
	case cmdLower == "/status" || cmdLower == "status":
		txt := formatStatusText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case cmdLower == "/scan" || cmdLower == "scan":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang memindai presensi aktif di seluruh mata kuliah...</i>", nil)
		res := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, res, telegram.GetBackKeyboard())
	case cmdLower == "/jadwal" || cmdLower == "jadwal":
		txt := formatJadwalText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case cmdLower == "/rekap" || cmdLower == "rekap":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang menghitung rekapitulasi kehadiran per mata kuliah...</i>", nil)
		txt := formatRekapDetail()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case cmdLower == "/tugas" || cmdLower == "tugas":
		txt := formatTugasText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case cmdLower == "/cooldown" || cmdLower == "/istirahat":
		state.mu.Lock()
		state.CooldownActive = true
		state.CooldownDate = getWIBNow().Format("2006-01-02")
		state.mu.Unlock()
		addLog("Mode cooldown diaktifkan")
		sendWelcomeMenu()
	case cmdLower == "/resume" || cmdLower == "resume":
		state.mu.Lock()
		state.CooldownActive = false
		state.ForceSiaga = true
		state.mu.Unlock()
		addLog("Mode siaga penuh diaktifkan")
		sendWelcomeMenu()
	default:
		sendWelcomeMenu()
	}
}

func handleTgAction(action string, chatID int64, msgID int, queryID string) {
	switch action {
	case "btn_page_1":
		sendWelcomeMenu()
	case "btn_page_2":
		text := getWelcomeText()
		kb := telegram.GetMainKeyboard(2, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)
	case "btn_scan":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang memindai presensi aktif di seluruh mata kuliah...</i>", nil)
		res := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, res, telegram.GetBackKeyboard())
	case "btn_jadwal":
		txt := formatJadwalText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case "btn_tugas":
		txt := formatTugasText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case "btn_rekap":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang menghitung rekapitulasi kehadiran per mata kuliah...</i>", nil)
		txt := formatRekapDetail()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case "btn_log":
		txt := formatLogsText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case "btn_cooldown":
		state.mu.Lock()
		state.CooldownActive = true
		state.CooldownDate = getWIBNow().Format("2006-01-02")
		state.mu.Unlock()
		addLog("Mode cooldown diaktifkan")
		sendWelcomeMenu()
	case "btn_resume":
		state.mu.Lock()
		state.CooldownActive = false
		state.ForceSiaga = true
		state.mu.Unlock()
		addLog("Mode siaga penuh diaktifkan")
		sendWelcomeMenu()
	case "btn_notif":
		count, lines, _ := state.PrimaryClient.GetUnreadNotifications()
		if count == 0 {
			state.TgBot.ShowContentCard(chatID, "<b>NOTIFIKASI ETHOL</b>\n\nTidak ada notifikasi baru yang belum dibaca.", telegram.GetBackKeyboard())
		} else {
			state.TgBot.ShowContentCard(chatID, fmt.Sprintf("<b>NOTIFIKASI ETHOL (%d Belum Dibaca)</b>\n\n%s", count, strings.Join(lines, "\n")), telegram.GetBackKeyboard())
		}
	case "btn_status":
		txt := formatStatusText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case "btn_relogin":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang merefresh otentikasi login ETHOL...</i>", nil)
		ok, err := state.PrimaryClient.LoginCAS()
		if ok {
			_ = state.PrimaryClient.UpdateCache(true)
			addLog("Sesi login disegarkan ulang")
			state.TgBot.ShowContentCard(chatID, "✅ <b>SESI DIPERBARUI</b>\nOtentikasi login dan sinkronisasi data kuliah berhasil disegarkan.", telegram.GetBackKeyboard())
		} else {
			state.TgBot.ShowContentCard(chatID, fmt.Sprintf("❌ <b>GAGAL REFRESH SESI</b>\n%v", err), telegram.GetBackKeyboard())
		}
	case "btn_konthol_public":
		state.TgBot.ShowContentCard(chatID, "<b>MODUL KON-THOL PUBLIC EDITION</b>\n\nKelola pemindaian serentak dan daftar akun mahasiswa terdaftar.", telegram.GetPublicKeyboard())
	case "btn_help":
		txt := formatHelpText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())
	case "btn_public_scan":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang menjalankan siklus scan serentak seluruh akun mahasiswa...</i>", telegram.GetPublicKeyboard())
		res := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, res, telegram.GetPublicKeyboard())
	case "btn_public_accounts":
		txt := formatPublicAccounts()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPublicKeyboard())
	}
}
