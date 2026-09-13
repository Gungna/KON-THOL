package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type UserInfo struct {
	Nomor       int    `json:"nomor"`
	NipNrp      string `json:"nipnrp"`
	Nama        string `json:"nama"`
	Email       string `json:"email"`
	Status      string `json:"status"`
	Program     string `json:"program"`
	Jurusan     string `json:"jurusan"`
	JenisSchema int    `json:"jenis_schema"`
}

type EtholConfig struct {
	TahunAktif    int `json:"tahun_aktif"`
	SemesterAktif int `json:"semester_aktif"`
}

type CourseItem struct {
	Nomor          int         `json:"nomor"`
	JenisSchema    int         `json:"jenis_schema"`
	NamaMatakuliah interface{} `json:"nama_matakuliah"`
	Matakuliah     interface{} `json:"matakuliah"`
	Dosen          string      `json:"dosen"`
	NomorDosen     interface{} `json:"nomor_dosen"`
	KuliahAsal     int         `json:"kuliah_asal"`
}

func (c *CourseItem) CourseName() string {
	if m, ok := c.NamaMatakuliah.(map[string]interface{}); ok {
		if n, ok := m["nama"].(string); ok && n != "" {
			return n
		}
	}
	if n, ok := c.NamaMatakuliah.(string); ok && n != "" {
		return n
	}
	if m, ok := c.Matakuliah.(map[string]interface{}); ok {
		if n, ok := m["nama"].(string); ok && n != "" {
			return n
		}
	}
	if n, ok := c.Matakuliah.(string); ok && n != "" {
		return n
	}
	return fmt.Sprintf("Kuliah #%d", c.Nomor)
}

type ScheduleItem struct {
	Nomor          int         `json:"nomor"`
	Kuliah         int         `json:"kuliah"`
	Hari           string      `json:"hari"`
	JamAwal        string      `json:"jam_awal"`
	JamAkhir       string      `json:"jam_akhir"`
	Ruang          string      `json:"ruang"`
	NamaMatakuliah interface{} `json:"nama_matakuliah"`
	Matakuliah     interface{} `json:"matakuliah"`
	Dosen          string      `json:"dosen"`
	NomorHari      int         `json:"nomor_hari"`
}

func (s *ScheduleItem) CourseName() string {
	if m, ok := s.NamaMatakuliah.(map[string]interface{}); ok {
		if n, ok := m["nama"].(string); ok && n != "" {
			return n
		}
	}
	if n, ok := s.NamaMatakuliah.(string); ok && n != "" {
		return n
	}
	if m, ok := s.Matakuliah.(map[string]interface{}); ok {
		if n, ok := m["nama"].(string); ok && n != "" {
			return n
		}
	}
	if n, ok := s.Matakuliah.(string); ok && n != "" {
		return n
	}
	if s.Dosen != "" {
		return s.Dosen
	}
	return fmt.Sprintf("Kuliah #%d", s.Nomor)
}

type AttendancePayload struct {
	Kuliah      int    `json:"kuliah"`
	JenisSchema int    `json:"jenis_schema"`
	Mahasiswa   int    `json:"mahasiswa"`
	Key         string `json:"key"`
	KuliahAsal  int    `json:"kuliah_asal"`
}

type AttendanceResponse struct {
	Sukses bool   `json:"sukses"`
	Pesan  string `json:"pesan"`
}

type CourseStatBreakdown struct {
	KuliahID int    `json:"kuliah_id"`
	Nama     string `json:"nama"`
	Hadir    int    `json:"hadir"`
	Total    int    `json:"total"`
	DToday   int    `json:"d_today"`
	MToday   int    `json:"m_today"`
}

type AttendanceStatistics struct {
	Percentage         float64               `json:"percentage"`
	TotalDosenSemester int                   `json:"total_dosen_semester"`
	TotalMhsSemester   int                   `json:"total_mhs_semester"`
	TotalDosenToday    int                   `json:"total_dosen_today"`
	TotalMhsToday      int                   `json:"total_mhs_today"`
	Breakdown          []CourseStatBreakdown `json:"breakdown"`
}

type TaskItem struct {
	KuliahID    int    `json:"kuliah_id"`
	TugasID     int    `json:"tugas_id"`
	Matkul      string `json:"matkul"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Deadline    string `json:"deadline"`
}

type EtholClient struct {
	Username      string
	Password      string
	HTTPClient    *http.Client
	UserInfo      *UserInfo
	EtholConfig   *EtholConfig
	CoursesCache  []CourseItem
	ScheduleCache []ScheduleItem
	LastAuthTime  time.Time
	mu            sync.RWMutex
}

func NewEtholClient(username, password string) (*EtholClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("init cookie jar failed: %w", err)
	}

	transport := &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	return &EtholClient{
		Username: username,
		Password: password,
		HTTPClient: &http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   45 * time.Second,
		},
	}, nil
}

func (c *EtholClient) LoginCAS() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("[ETHOL] Melakukan otentikasi CAS SSO PENS untuk %s...", c.Username)

	casURL := "https://ethol.pens.ac.id/api/auth/cas-redirect"
	req, err := http.NewRequest(http.MethodGet, casURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("cas-redirect request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read cas response failed: %w", err)
	}

	actionURL, formData, found := parseCASForm(string(bodyBytes), resp.Request.URL)
	if !found {
		valid, info := c.checkTokenUnsafe()
		if valid {
			c.UserInfo = info
			c.LastAuthTime = time.Now()
			log.Printf("[ETHOL] Sesi CAS sudah aktif: %s (%s)", info.Nama, info.NipNrp)
			return true, nil
		}
		return false, fmt.Errorf("form login CAS tidak ditemukan pada gateway SSO")
	}

	formData.Set("username", c.Username)
	formData.Set("password", c.Password)
	formData.Set("_eventId", "submit")
	formData.Set("submit", "LOGIN")

	postReq, err := http.NewRequest(http.MethodPost, actionURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return false, err
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	postResp, err := c.HTTPClient.Do(postReq)
	if err != nil {
		return false, fmt.Errorf("cas submit request failed: %w", err)
	}
	defer postResp.Body.Close()

	valid, info := c.checkTokenUnsafe()
	if !valid {
		return false, fmt.Errorf("validasi token gagal setelah login CAS")
	}

	c.UserInfo = info
	c.LastAuthTime = time.Now()
	log.Printf("[ETHOL] Login CAS Berhasil: %s (%s)", info.Nama, info.NipNrp)
	return true, nil
}

func (c *EtholClient) checkTokenUnsafe() (bool, *UserInfo) {
	valReq, err := http.NewRequest(http.MethodGet, "https://ethol.pens.ac.id/api/auth/validasi-token", nil)
	if err != nil {
		return false, nil
	}
	valReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	valResp, err := c.HTTPClient.Do(valReq)
	if err != nil || valResp.StatusCode != http.StatusOK {
		return false, nil
	}
	defer valResp.Body.Close()

	var info UserInfo
	if err := json.NewDecoder(valResp.Body).Decode(&info); err != nil {
		return false, nil
	}

	if info.Nomor > 0 || info.Nama != "" {
		return true, &info
	}
	return false, nil
}

func (c *EtholClient) CheckToken() (bool, *UserInfo) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkTokenUnsafe()
}

func (c *EtholClient) RefreshSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, err := http.NewRequest(http.MethodPost, "https://ethol.pens.ac.id/api/auth/refresh", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTPClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return true
	}
	if resp != nil {
		resp.Body.Close()
	}
	return false
}

func (c *EtholClient) UpdateCache(force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tahunAktif := time.Now().Year()
	semesterAktif := 1

	confReq, _ := http.NewRequest(http.MethodGet, "https://ethol.pens.ac.id/api/auth/config", nil)
	confReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	confResp, err := c.HTTPClient.Do(confReq)
	if err == nil && confResp.StatusCode == http.StatusOK {
		var raw map[string]interface{}
		if err := json.NewDecoder(confResp.Body).Decode(&raw); err == nil {
			if t, ok := raw["tahun_aktif"].(float64); ok && t > 2000 {
				tahunAktif = int(t)
			}
			if s, ok := raw["semester_aktif"].(float64); ok && s > 0 {
				semesterAktif = int(s)
			}
		}
		confResp.Body.Close()
	} else if confResp != nil {
		confResp.Body.Close()
	}

	c.EtholConfig = &EtholConfig{
		TahunAktif:    tahunAktif,
		SemesterAktif: semesterAktif,
	}

	cURL := fmt.Sprintf("https://ethol.pens.ac.id/api/kuliah?tahun=%d&semester=%d", tahunAktif, semesterAktif)
	cReq, _ := http.NewRequest(http.MethodGet, cURL, nil)
	cReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	cResp, err := c.HTTPClient.Do(cReq)
	if err == nil && cResp.StatusCode == http.StatusOK {
		var courses []CourseItem
		if err := json.NewDecoder(cResp.Body).Decode(&courses); err == nil && len(courses) > 0 {
			c.CoursesCache = courses
		}
		cResp.Body.Close()
	} else if cResp != nil {
		cResp.Body.Close()
	}

	jURL := fmt.Sprintf("https://ethol.pens.ac.id/api/jadwal/jadwal-online?tahun=%d&semester=%d", tahunAktif, semesterAktif)
	jReq, _ := http.NewRequest(http.MethodGet, jURL, nil)
	jReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	jResp, err := c.HTTPClient.Do(jReq)
	if err == nil && jResp.StatusCode == http.StatusOK {
		var schedules []ScheduleItem
		if err := json.NewDecoder(jResp.Body).Decode(&schedules); err == nil && len(schedules) > 0 {
			c.ScheduleCache = schedules
		}
		jResp.Body.Close()
	} else if jResp != nil {
		jResp.Body.Close()
	}

	log.Printf("[ETHOL] Cache diperbarui: %d mata kuliah, %d jadwal aktif (Tahun %d, Sem %d)",
		len(c.CoursesCache), len(c.ScheduleCache), tahunAktif, semesterAktif)
	return nil
}

func (c *EtholClient) GetCourses() []CourseItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.CoursesCache
}

func (c *EtholClient) GetSchedules() []ScheduleItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ScheduleCache
}

func (c *EtholClient) CheckActivePresence(courseID, jenisSchema int) (string, error) {
	u := fmt.Sprintf("https://ethol.pens.ac.id/api/presensi/aktif-kuliah?kuliah=%d&jenis_schema=%d", courseID, jenisSchema)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", err
	}

	switch v := raw.(type) {
	case []interface{}:
		if len(v) > 0 {
			if item, ok := v[0].(map[string]interface{}); ok {
				if k, ok := item["key"].(string); ok && k != "" {
					return k, nil
				}
			}
		}
	case map[string]interface{}:
		if k, ok := v["key"].(string); ok && k != "" {
			return k, nil
		}
	}

	return "", nil
}

func (c *EtholClient) SubmitAttendance(courseID, jenisSchema int, key string, kuliahAsal int) (bool, string, error) {
	c.mu.RLock()
	mahasiswaID := 0
	if c.UserInfo != nil {
		mahasiswaID = c.UserInfo.Nomor
	}
	c.mu.RUnlock()

	if kuliahAsal <= 0 {
		kuliahAsal = courseID
	}

	payload := AttendancePayload{
		Kuliah:      courseID,
		JenisSchema: jenisSchema,
		Mahasiswa:   mahasiswaID,
		Key:         key,
		KuliahAsal:  kuliahAsal,
	}

	pBytes, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, "https://ethol.pens.ac.id/api/presensi/mahasiswa", bytes.NewReader(pBytes))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	var res AttendanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false, "", err
	}

	success := res.Sukses || strings.Contains(strings.ToLower(res.Pesan), "sudah")
	return success, res.Pesan, nil
}

func (c *EtholClient) GetAttendanceStatistics(todayDateStr string) (*AttendanceStatistics, error) {
	c.mu.RLock()
	userInfo := c.UserInfo
	courses := c.CoursesCache
	ethCfg := c.EtholConfig
	c.mu.RUnlock()

	if userInfo == nil || len(courses) == 0 {
		return nil, fmt.Errorf("user info atau courses cache belum tersedia")
	}

	nomorMhs := userInfo.Nomor
	tahunAktif := time.Now().Year()
	semesterAktif := 1
	if ethCfg != nil && ethCfg.TahunAktif > 2000 {
		tahunAktif = ethCfg.TahunAktif
		semesterAktif = ethCfg.SemesterAktif
	}

	totalDosenSemester := 0
	totalMhsSemester := 0
	totalDosenToday := 0
	totalMhsToday := 0

	var breakdown []CourseStatBreakdown

	for _, cr := range courses {
		kID := cr.Nomor
		schema := cr.JenisSchema
		mkName := cr.CourseName()

		mhsURL := fmt.Sprintf("https://ethol.pens.ac.id/api/presensi/riwayat?kuliah=%d&jenis_schema=%d&nomor=%d", kID, schema, nomorMhs)
		mhsReq, _ := http.NewRequest(http.MethodGet, mhsURL, nil)
		mhsReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		mhsResp, err := c.HTTPClient.Do(mhsReq)
		var mhsList []map[string]interface{}
		if err == nil && mhsResp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(mhsResp.Body).Decode(&mhsList)
			mhsResp.Body.Close()
		} else if mhsResp != nil {
			mhsResp.Body.Close()
		}

		dosenURL := fmt.Sprintf("https://ethol.pens.ac.id/api/presensi/get-tanggal-presensi-dosen-per-semester?tahun=%d&semester=%d&kuliah=%d&dosen=%v",
			tahunAktif, semesterAktif, kID, cr.NomorDosen)
		dosenReq, _ := http.NewRequest(http.MethodGet, dosenURL, nil)
		dosenReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		dosenResp, err := c.HTTPClient.Do(dosenReq)
		var dosenList []map[string]interface{}
		if err == nil && dosenResp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(dosenResp.Body).Decode(&dosenList)
			dosenResp.Body.Close()
		} else if dosenResp != nil {
			dosenResp.Body.Close()
		}

		dToday := 0
		for _, d := range dosenList {
			w1 := fmt.Sprintf("%v", d["waktu_indonesia"])
			w2 := fmt.Sprintf("%v", d["waktu"])
			if strings.Contains(w1, todayDateStr) || strings.Contains(w2, todayDateStr) {
				dToday++
			}
		}

		mToday := 0
		for _, m := range mhsList {
			t1 := fmt.Sprintf("%v", m["tanggal"])
			t2 := fmt.Sprintf("%v", m["waktu_indonesia"])
			if strings.Contains(t1, todayDateStr) || strings.Contains(t2, todayDateStr) {
				mToday++
			}
		}

		totalDosenSemester += len(dosenList)
		totalMhsSemester += len(mhsList)
		totalDosenToday += dToday
		totalMhsToday += mToday

		breakdown = append(breakdown, CourseStatBreakdown{
			KuliahID: kID,
			Nama:     mkName,
			Hadir:    len(mhsList),
			Total:    len(dosenList),
			DToday:   dToday,
			MToday:   mToday,
		})
	}

	pct := 100.0
	if totalDosenSemester > 0 {
		pct = (float64(totalMhsSemester) / float64(totalDosenSemester)) * 100.0
	}

	return &AttendanceStatistics{
		Percentage:         pct,
		TotalDosenSemester: totalDosenSemester,
		TotalMhsSemester:   totalMhsSemester,
		TotalDosenToday:    totalDosenToday,
		TotalMhsToday:      totalMhsToday,
		Breakdown:          breakdown,
	}, nil
}

func (c *EtholClient) GetPendingTasks() ([]TaskItem, error) {
	c.mu.RLock()
	courses := c.CoursesCache
	c.mu.RUnlock()

	var pending []TaskItem
	for _, cr := range courses {
		kID := cr.Nomor
		schema := cr.JenisSchema
		mkName := cr.CourseName()

		tURL := fmt.Sprintf("https://ethol.pens.ac.id/api/tugas?kuliah=%d&jenisSchema=%d", kID, schema)
		req, _ := http.NewRequest(http.MethodGet, tURL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		resp, err := c.HTTPClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var tasks []map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&tasks)
		resp.Body.Close()

		for _, t := range tasks {
			subTime := t["submission_time"]
			tutup := fmt.Sprintf("%v", t["tutup"])
			if subTime == nil && tutup != "1" {
				title := fmt.Sprintf("%v", t["title"])
				if title == "<nil>" || title == "" {
					title = fmt.Sprintf("%v", t["judul"])
				}
				desc := fmt.Sprintf("%v", t["description"])
				if desc == "<nil>" {
					desc = fmt.Sprintf("%v", t["deskripsi"])
				}
				dl := fmt.Sprintf("%v", t["deadline_indonesia"])
				if dl == "<nil>" || dl == "" {
					dl = fmt.Sprintf("%v", t["deadline"])
				}

				tID := 0
				if num, ok := t["id"].(float64); ok {
					tID = int(num)
				}

				pending = append(pending, TaskItem{
					KuliahID:    kID,
					TugasID:     tID,
					Matkul:      mkName,
					Title:       title,
					Description: desc,
					Deadline:    dl,
				})
			}
		}
	}
	return pending, nil
}

func (c *EtholClient) GetUnreadNotifications() (int, []string, error) {
	nURL := "https://ethol.pens.ac.id/api/notifikasi/mahasiswa?filterNotif=SEMUA"
	nReq, _ := http.NewRequest(http.MethodGet, nURL, nil)
	nReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	nResp, err := c.HTTPClient.Do(nReq)
	if err != nil || nResp.StatusCode != http.StatusOK {
		if nResp != nil {
			nResp.Body.Close()
		}
		return 0, nil, err
	}
	defer nResp.Body.Close()

	var notifs []map[string]interface{}
	_ = json.NewDecoder(nResp.Body).Decode(&notifs)

	var lines []string
	for _, n := range notifs {
		ket := fmt.Sprintf("%v", n["keterangan"])
		tgl := fmt.Sprintf("%v", n["createdAtIndonesia"])
		if tgl == "<nil>" || tgl == "" {
			tgl = fmt.Sprintf("%v", n["waktuNotifikasi"])
		}
		if ket != "<nil>" && ket != "" {
			lines = append(lines, fmt.Sprintf("🔔 <b>%s</b>\n   🕒 <code>%s</code>", ket, tgl))
		}
	}

	return len(lines), lines, nil
}

func parseCASForm(htmlContent string, baseURL *url.URL) (string, url.Values, bool) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, false
	}

	var formNode *html.Node
	var findForm func(*html.Node)
	findForm = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == "fm1" {
					formNode = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findForm(c)
			if formNode != nil {
				return
			}
		}
	}
	findForm(doc)

	if formNode == nil {
		return "", nil, false
	}

	var action string
	for _, a := range formNode.Attr {
		if a.Key == "action" {
			action = a.Val
			break
		}
	}

	fullAction := action
	if rel, err := url.Parse(action); err == nil && baseURL != nil {
		fullAction = baseURL.ResolveReference(rel).String()
	}

	values := url.Values{}
	var findInputs func(*html.Node)
	findInputs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			var name, val string
			for _, a := range n.Attr {
				if a.Key == "name" {
					name = a.Val
				} else if a.Key == "value" {
					val = a.Val
				}
			}
			if name != "" {
				values.Set(name, val)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findInputs(c)
		}
	}
	findInputs(formNode)

	return fullAction, values, true
}
