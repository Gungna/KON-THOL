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
	KuliahAsal     int         `json:"kuliah_asal"`
}

func (c *CourseItem) CourseName() string {
	if m, ok := c.NamaMatakuliah.(map[string]interface{}); ok {
		if n, ok := m["nama"].(string); ok {
			return n
		}
	}
	if n, ok := c.NamaMatakuliah.(string); ok && n != "" {
		return n
	}
	if m, ok := c.Matakuliah.(map[string]interface{}); ok {
		if n, ok := m["nama"].(string); ok {
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
	Hari           string      `json:"hari"`
	JamMulai       string      `json:"jam_mulai"`
	JamSelesai     string      `json:"jam_selesai"`
	Ruang          string      `json:"ruang"`
	NamaMatakuliah interface{} `json:"nama_matakuliah"`
	Matakuliah     interface{} `json:"matakuliah"`
	Dosen          string      `json:"dosen"`
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
			Timeout:   15 * time.Second,
		},
	}, nil
}

func (c *EtholClient) LoginCAS() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("[ETHOL] Melakukan otentikasi CAS SSO PENS untuk %s...", c.Username)

	// 1. GET cas-redirect
	casURL := "https://ethol.pens.ac.id/api/auth/cas-redirect"
	req, err := http.NewRequest(http.MethodGet, casURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("cas-redirect request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read cas response failed: %w", err)
	}

	// 2. Parse HTML form with id 'fm1'
	actionURL, formData, found := parseCASForm(string(bodyBytes), resp.Request.URL)
	if !found {
		// Cek apakah token sudah valid
		valid, info := c.checkTokenUnsafe()
		if valid {
			c.UserInfo = info
			c.LastAuthTime = time.Now()
			log.Printf("[ETHOL] Sesi CAS sudah aktif: %s (%s)", info.Nama, info.NipNrp)
			return true, nil
		}
		return false, fmt.Errorf("form login CAS tidak ditemukan pada gateway SSO")
	}

	// 3. Populate form credentials
	formData.Set("username", c.Username)
	formData.Set("password", c.Password)
	formData.Set("_eventId", "submit")
	formData.Set("submit", "LOGIN")

	postReq, err := http.NewRequest(http.MethodPost, actionURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return false, err
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	postResp, err := c.HTTPClient.Do(postReq)
	if err != nil {
		return false, fmt.Errorf("cas submit request failed: %w", err)
	}
	defer postResp.Body.Close()

	// 4. Validate Token
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
	valReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0")

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

	// 1. Fetch Config
	confReq, _ := http.NewRequest(http.MethodGet, "https://ethol.pens.ac.id/api/auth/config", nil)
	confResp, err := c.HTTPClient.Do(confReq)
	if err != nil {
		return err
	}
	defer confResp.Body.Close()

	var cfg EtholConfig
	if err := json.NewDecoder(confResp.Body).Decode(&cfg); err != nil {
		cfg.TahunAktif = time.Now().Year()
		cfg.SemesterAktif = 1
	}
	c.EtholConfig = &cfg

	// 2. Fetch Courses
	cURL := fmt.Sprintf("https://ethol.pens.ac.id/api/kuliah?tahun=%d&semester=%d", cfg.TahunAktif, cfg.SemesterAktif)
	cReq, _ := http.NewRequest(http.MethodGet, cURL, nil)
	cResp, err := c.HTTPClient.Do(cReq)
	if err == nil && cResp.StatusCode == http.StatusOK {
		defer cResp.Body.Close()
		var courses []CourseItem
		if err := json.NewDecoder(cResp.Body).Decode(&courses); err == nil {
			c.CoursesCache = courses
		}
	} else if cResp != nil {
		cResp.Body.Close()
	}

	// 3. Fetch Schedule
	jURL := fmt.Sprintf("https://ethol.pens.ac.id/api/jadwal/jadwal-online?tahun=%d&semester=%d", cfg.TahunAktif, cfg.SemesterAktif)
	jReq, _ := http.NewRequest(http.MethodGet, jURL, nil)
	jResp, err := c.HTTPClient.Do(jReq)
	if err == nil && jResp.StatusCode == http.StatusOK {
		defer jResp.Body.Close()
		var schedules []ScheduleItem
		if err := json.NewDecoder(jResp.Body).Decode(&schedules); err == nil {
			c.ScheduleCache = schedules
		}
	} else if jResp != nil {
		jResp.Body.Close()
	}

	log.Printf("[ETHOL] Cache diperbarui: %d mata kuliah, %d jadwal aktif (Tahun %d, Sem %d)",
		len(c.CoursesCache), len(c.ScheduleCache), cfg.TahunAktif, cfg.SemesterAktif)
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

	// Extract key from JSON (could be array or object)
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
