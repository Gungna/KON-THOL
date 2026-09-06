#!/usr/bin/env python3
"""
KON-THOL (Public Edition) - Kawan Otomasi dan Notifikasi untuk E-THOL PENS
========================================================================
Lightweight, Multi-Account, Concurrent Anti-Detection Daemon for ETHOL PENS
Repository: kon-thol (Public)

Features:
- Multi-Account Concurrency: Run 1, 2, or more student accounts simultaneously using ThreadPoolExecutor
- WhatsApp Gateway Integration: Real-time alerts via WhatsApp (Fonnte, WPPConnect, Baileys Webhook) + Telegram fallback
- Independent Sessions: Each account maintains its own isolated cookies, session headers, and attendance cache
- Anti-Detection Jitter: Human-like access patterns, random intervals, and robust auto-relogin
"""

import os
import sys
import json
import time
import datetime
import random
import logging
import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import Dict, Any, List, Optional

WIB = datetime.timezone(datetime.timedelta(hours=7))

def get_wib_now():
    return datetime.datetime.now(WIB)

def get_wib_str():
    return get_wib_now().strftime("%Y-%m-%d %H:%M:%S WIB")

class WIBFormatter(logging.Formatter):
    def formatTime(self, record, datefmt=None):
        dt = datetime.datetime.fromtimestamp(record.created, WIB)
        return dt.strftime(datefmt or "%Y-%m-%d %H:%M:%S WIB")

# Setup Logging
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
LOG_FILE = os.path.join(BASE_DIR, "kon_thol.log")

logger = logging.getLogger("kon_thol_public")
logger.setLevel(logging.INFO)
file_handler = logging.FileHandler(LOG_FILE, encoding='utf-8')
file_handler.setFormatter(WIBFormatter("%(asctime)s [%(levelname)s] %(message)s"))
stream_handler = logging.StreamHandler(sys.stdout)
stream_handler.setFormatter(WIBFormatter("%(asctime)s [%(levelname)s] %(message)s"))

if not logger.handlers:
    logger.addHandler(file_handler)
    logger.addHandler(stream_handler)

CONFIG_PATH = os.path.join(BASE_DIR, "credentials.json")
ACCOUNTS_PATH = os.path.join(BASE_DIR, "accounts.json")

class NotificationDispatcher:
    """Dispatches notifications to WhatsApp and Telegram."""
    def __init__(self, wa_config: Dict[str, Any], tg_config: Dict[str, Any]):
        self.wa_config = wa_config
        self.tg_config = tg_config

    def send_whatsapp(self, phone: str, text: str) -> bool:
        if not phone:
            phone = self.wa_config.get("target_phone", "")
        if not phone:
            return False

        provider = self.wa_config.get("provider", "fonnte").lower()
        api_key = self.wa_config.get("api_key", "")
        endpoint = self.wa_config.get("endpoint_url", "https://api.fonnte.com/send")

        try:
            if provider == "fonnte":
                headers = {"Authorization": api_key}
                payload = {
                    "target": phone,
                    "message": text,
                    "countryCode": "62"
                }
                res = requests.post(endpoint, headers=headers, data=payload, timeout=10)
                if res.status_code in [200, 201]:
                    logger.info(f"[WA] Berhasil kirim WA ke {phone}")
                    return True
                else:
                    logger.warning(f"[WA] Gagal kirim WA via Fonnte: HTTP {res.status_code} - {res.text}")
            elif provider in ["webhook", "generic", "wppconnect"]:
                headers = {"Content-Type": "application/json"}
                if api_key:
                    headers["Authorization"] = f"Bearer {api_key}"
                payload = {"phone": phone, "message": text}
                res = requests.post(endpoint, headers=headers, json=payload, timeout=10)
                return res.status_code in [200, 201]
        except Exception as e:
            logger.error(f"[WA] Exception kirim WhatsApp: {e}")
        return False

    def send_telegram(self, chat_id: str, text: str) -> bool:
        bot_token = self.tg_config.get("token", "")
        if not chat_id or not bot_token:
            return False
        try:
            url = f"https://api.telegram.org/bot{bot_token}/sendMessage"
            res = requests.post(url, json={"chat_id": chat_id, "text": text, "parse_mode": "HTML"}, timeout=10)
            return res.status_code == 200
        except Exception as e:
            logger.error(f"[TG] Exception kirim Telegram: {e}")
            return False

    def broadcast(self, user_name: str, wa_phone: str, tg_chat_id: str, title: str, message: str, status: str = "INFO"):
        icon = "✅" if status == "SUCCESS" else ("⚠️" if status == "WARN" else "ℹ️")
        wa_text = f"*{icon} [kon-thol: {user_name}]* {title}\n\n{message}\n\n_Waktu: {get_wib_str()}_"
        tg_text = f"<b>{icon} [kon-thol: {user_name}]</b> {title}\n\n{message}\n\n<i>{get_wib_str()}</i>"

        # Kirim WhatsApp
        if wa_phone or self.wa_config.get("target_phone"):
            self.send_whatsapp(wa_phone, wa_text)

        # Kirim Telegram fallback
        if tg_chat_id or self.tg_config.get("chat_id"):
            self.send_telegram(tg_chat_id or self.tg_config.get("chat_id"), tg_text)

class StudentWorker:
    """Individual worker representing one student's session & presence checker."""
    ETHOL_BASE = "https://ethol.pens.ac.id"

    def __init__(self, account_config: Dict[str, Any], dispatcher: NotificationDispatcher):
        self.config = account_config
        self.dispatcher = dispatcher
        self.name = account_config.get("name", "Student")
        self.username = account_config.get("username", "")
        self.password = account_config.get("password", "")
        self.wa_phone = account_config.get("wa_target", "")
        self.tg_chat_id = str(account_config.get("telegram_chat_id", ""))

        # Isolated HTTP session
        self.session = requests.Session()
        retries = Retry(total=2, backoff_factor=0.5, status_forcelist=[500, 502, 503, 504])
        adapter = HTTPAdapter(max_retries=retries, pool_connections=5, pool_maxsize=10)
        self.session.mount("https://", adapter)
        self.session.mount("http://", adapter)
        self.session.headers.update({
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36',
            'Accept': 'application/json, text/plain, */*',
            'Accept-Language': 'id-ID,id;q=0.9,en-US;q=0.8',
            'Origin': 'https://ethol.pens.ac.id',
            'Referer': 'https://ethol.pens.ac.id/'
        })

        self.user_data = None
        self.token = None
        # State file per student
        safe_user = "".join(c for c in self.username if c.isalnum())
        self.state_file = os.path.join(BASE_DIR, f"attended_{safe_user}.json")
        self.attended_keys = set()
        self.load_attended()

    def load_attended(self):
        if os.path.exists(self.state_file):
            try:
                with open(self.state_file, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    self.attended_keys = set(data.get("attended", []))
            except Exception:
                self.attended_keys = set()

    def save_attended(self):
        try:
            with open(self.state_file, "w", encoding="utf-8") as f:
                json.dump({"attended": list(self.attended_keys), "updated": get_wib_str()}, f, indent=2)
        except Exception as e:
            logger.warning(f"[{self.name}] Gagal menyimpan attended state: {e}")

    def login(self) -> bool:
        login_url = f"{self.ETHOL_BASE}/api/auth/login"
        payload = {"username": self.username, "password": self.password}
        try:
            res = self.session.post(login_url, json=payload, timeout=12)
            if res.status_code == 200:
                data = res.json()
                self.token = data.get("token") or data.get("access_token")
                if self.token:
                    self.session.headers["Authorization"] = f"Bearer {self.token}"
                self.user_data = data.get("user") or data.get("data")
                logger.info(f"[{self.name}] Berhasil login ke ETHOL PENS.")
                return True
            else:
                logger.warning(f"[{self.name}] Login gagal: HTTP {res.status_code} - {res.text[:100]}")
                return False
        except Exception as e:
            logger.error(f"[{self.name}] Exception saat login: {e}")
            return False

    def scan_and_attend(self):
        """Scans current active courses and performs auto-presence."""
        if not self.token and not self.login():
            return

        today_str = get_wib_now().strftime("%Y-%m-%d")
        now_wib = get_wib_now()

        # Fetch active courses for student
        try:
            kuliah_url = f"{self.ETHOL_BASE}/api/kuliah/mahasiswa/jadwal-hari-ini"
            res = self.session.get(kuliah_url, timeout=12)
            
            # If unauthorized, re-login once
            if res.status_code in [401, 403]:
                if self.login():
                    res = self.session.get(kuliah_url, timeout=12)
                else:
                    return

            if res.status_code != 200:
                # Fallback to general list if today endpoint not ready
                kuliah_url = f"{self.ETHOL_BASE}/api/kuliah/mahasiswa"
                res = self.session.get(kuliah_url, timeout=12)

            if res.status_code != 200:
                logger.debug(f"[{self.name}] Status kuliah: {res.status_code}")
                return

            courses = res.json()
            if isinstance(courses, dict):
                courses = courses.get("data") or courses.get("jadwal") or []

            logger.info(f"[{self.name}] Memeriksa {len(courses)} jadwal kuliah hari ini...")

            for item in courses:
                matkul_nama = item.get("matakuliah") or item.get("nama") or item.get("course_name") or "Mata Kuliah"
                kuliah_id = item.get("id_kuliah") or item.get("id")
                pertemuan = item.get("pertemuan") or item.get("minggu") or 1
                unique_key = f"{today_str}_{kuliah_id}_{pertemuan}"

                if unique_key in self.attended_keys:
                    continue

                # Check if attendance is open
                presensi_open = item.get("status_presensi") == 1 or item.get("is_open") or item.get("buka_presensi")
                
                if presensi_open:
                    logger.info(f"[{self.name}] 🚨 PRESENSI TERBUKA: {matkul_nama} (Pertemuan {pertemuan})")
                    attend_res = self.session.post(
                        f"{self.ETHOL_BASE}/api/presensi/mahasiswa/masuk",
                        json={"id_kuliah": kuliah_id, "pertemuan": pertemuan},
                        timeout=12
                    )
                    
                    if attend_res.status_code in [200, 201]:
                        self.attended_keys.add(unique_key)
                        self.save_attended()
                        logger.info(f"[{self.name}] ✅ PRESENSI SUKSES: {matkul_nama}")
                        
                        # Trigger WhatsApp & Telegram Notification
                        self.dispatcher.broadcast(
                            user_name=self.name,
                            wa_phone=self.wa_phone,
                            tg_chat_id=self.tg_chat_id,
                            title="Presensi Berhasil!",
                            message=f"Mata Kuliah: *{matkul_nama}*\nPertemuan: {pertemuan}\nAkun: {self.username}",
                            status="SUCCESS"
                        )
                    else:
                        logger.warning(f"[{self.name}] Gagal presensi {matkul_nama}: {attend_res.text}")
        except Exception as e:
            logger.error(f"[{self.name}] Error saat scan presensi: {e}")

class KonTholEngine:
    """Main Orchestrator running multi-account workers concurrently."""
    def __init__(self):
        self.accounts_config = []
        self.wa_config = {}
        self.tg_config = {}
        self.load_all_configurations()
        self.dispatcher = NotificationDispatcher(self.wa_config, self.tg_config)
        self.workers = [StudentWorker(acc, self.dispatcher) for acc in self.accounts_config]

    def load_all_configurations(self):
        # 1. Load accounts.json if exists
        if os.path.exists(ACCOUNTS_PATH):
            with open(ACCOUNTS_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
                self.accounts_config = data.get("accounts", [])
                self.wa_config = data.get("whatsapp", {})
                self.tg_config = data.get("telegram", {})
        
        # 2. Fallback / Merge with credentials.json
        elif os.path.exists(CONFIG_PATH):
            with open(CONFIG_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
                if isinstance(data, list):
                    self.accounts_config = data
                elif "accounts" in data:
                    self.accounts_config = data.get("accounts", [])
                    self.wa_config = data.get("whatsapp", {})
                    self.tg_config = data.get("telegram", {})
                else:
                    self.accounts_config = [data]
                    self.tg_config = {
                        "token": data.get("telegram_token", ""),
                        "chat_id": data.get("telegram_chat_id", "")
                    }
                    self.wa_config = data.get("whatsapp", {})
        else:
            # Create sample accounts.json
            sample = {
                "whatsapp": {
                    "provider": "fonnte",
                    "api_key": "YOUR_FONNTE_API_KEY",
                    "target_phone": "6281234567890",
                    "endpoint_url": "https://api.fonnte.com/send"
                },
                "telegram": {
                    "token": "",
                    "chat_id": ""
                },
                "accounts": [
                    {
                        "name": "Mahasiswa 1",
                        "username": "nrp1@iet.student.pens.ac.id",
                        "password": "Password123",
                        "wa_target": "6281234567890",
                        "telegram_chat_id": ""
                    },
                    {
                        "name": "Mahasiswa 2",
                        "username": "nrp2@iet.student.pens.ac.id",
                        "password": "Password456",
                        "wa_target": "6289876543210",
                        "telegram_chat_id": ""
                    }
                ]
            }
            with open(ACCOUNTS_PATH, "w", encoding="utf-8") as f:
                json.dump(sample, f, indent=4)
            print(f"[*] Berkas konfigurasi contoh telah dibuat di: {ACCOUNTS_PATH}")
            print("[*] Silakan isi kredensial akun dan WhatsApp Gateway Anda.")
            sys.exit(0)

    def run_cycle(self):
        """Runs one check cycle concurrently across all registered student workers."""
        logger.info(f"--- [SIKLUS SCAN DIMULAI: {len(self.workers)} AKUN] ---")
        max_workers = min(len(self.workers) or 1, 10)
        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            futures = [executor.submit(worker.scan_and_attend) for worker in self.workers]
            for fut in as_completed(futures):
                try:
                    fut.result()
                except Exception as e:
                    logger.error(f"Worker thread error: {e}")
        logger.info(f"--- [SIKLUS SCAN SELESAI] ---\n")

    def run_daemon(self, interval_seconds: int = 300):
        print("==================================================================")
        print(" KON-THOL Engine (Public Edition) - Multi-Account & WhatsApp Bot")
        print(f" Terdaftar: {len(self.workers)} akun mahasiswa")
        print("==================================================================")
        
        while True:
            try:
                now = get_wib_now()
                # Active university hours: Senin - Jumat, 06:30 - 18:00 WIB
                is_weekday = now.weekday() < 5
                is_active_hours = 6 <= now.hour <= 18

                if is_weekday and is_active_hours:
                    self.run_cycle()
                else:
                    logger.info(f"[IDLE] Di luar jam kuliah ({get_wib_str()}). Siaga malam...")

                # Sleep with random jitter
                jitter = random.randint(-20, 40)
                sleep_time = max(60, interval_seconds + jitter)
                time.sleep(sleep_time)
            except KeyboardInterrupt:
                print("\n[!] Bot dihentikan oleh user.")
                break
            except Exception as e:
                logger.error(f"Main loop exception: {e}")
                time.sleep(30)

if __name__ == "__main__":
    interval = 300
    if len(sys.argv) > 1 and sys.argv[1].isdigit():
        interval = int(sys.argv[1])
    engine = KonTholEngine()
    engine.run_daemon(interval_seconds=interval)
