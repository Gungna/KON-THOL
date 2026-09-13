use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use scraper::{Html, Selector};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Serialize, Deserialize)]
pub struct ScheduleItem {
    pub hari: Option<String>,
    #[serde(rename = "jamAwal")]
    pub jam_awal: Option<String>,
    #[serde(rename = "jamAkhir")]
    pub jam_akhir: Option<String>,
    #[serde(rename = "matakuliah")]
    pub matakuliah: Option<String>,
    #[serde(rename = "nama")]
    pub nama: Option<String>,
    pub ruang: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct MatchResult {
    pub is_active: bool,
    pub course_name: String,
    pub start_time: String,
    pub end_time: String,
    pub room: String,
}

/// Parse CAS login HTML and extract form action and hidden fields with Rust speed
#[no_mangle]
pub extern "C" fn konthol_parse_cas_form(
    html_ptr: *const c_char,
    out_action: *mut *mut c_char,
    out_fields_json: *mut *mut c_char,
) -> i32 {
    if html_ptr.is_null() || out_action.is_null() || out_fields_json.is_null() {
        return -1;
    }

    let c_str = unsafe { CStr::from_ptr(html_ptr) };
    let html_content = match c_str.to_str() {
        Ok(s) => s,
        Err(_) => return -2,
    };

    let document = Html::parse_document(html_content);
    let form_selector = match Selector::parse("form#fm1, form") {
        Ok(s) => s,
        Err(_) => return -3,
    };

    let form_elem = match document.select(&form_selector).next() {
        Some(elem) => elem,
        None => return 0, // Form not found
    };

    let action = form_elem.value().attr("action").unwrap_or("").to_string();

    let input_selector = match Selector::parse("input") {
        Ok(s) => s,
        Err(_) => return -4,
    };

    let mut fields: HashMap<String, String> = HashMap::new();
    for input in form_elem.select(&input_selector) {
        if let Some(name) = input.value().attr("name") {
            if name != "reset" {
                let value = input.value().attr("value").unwrap_or("");
                fields.insert(name.to_string(), value.to_string());
            }
        }
    }

    let action_c = match CString::new(action) {
        Ok(s) => s,
        Err(_) => return -5,
    };

    let json_fields = match serde_json::to_string(&fields) {
        Ok(j) => j,
        Err(_) => return -6,
    };

    let json_c = match CString::new(json_fields) {
        Ok(s) => s,
        Err(_) => return -7,
    };

    unsafe {
        *out_action = action_c.into_raw();
        *out_fields_json = json_c.into_raw();
    }

    1 // Success
}

/// Zero-allocation match check for real-time active academic classes
#[no_mangle]
pub extern "C" fn konthol_match_active_schedule(
    schedules_json_ptr: *const c_char,
    day_ptr: *const c_char,
    time_ptr: *const c_char,
) -> *mut c_char {
    if schedules_json_ptr.is_null() || day_ptr.is_null() || time_ptr.is_null() {
        return std::ptr::null_mut();
    }

    let json_str = match unsafe { CStr::from_ptr(schedules_json_ptr) }.to_str() {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };
    let target_day = match unsafe { CStr::from_ptr(day_ptr) }.to_str() {
        Ok(s) => s.trim().to_lowercase(),
        Err(_) => return std::ptr::null_mut(),
    };
    let current_time = match unsafe { CStr::from_ptr(time_ptr) }.to_str() {
        Ok(s) => s.trim(),
        Err(_) => return std::ptr::null_mut(),
    };

    let items: Vec<ScheduleItem> = match serde_json::from_str(json_str) {
        Ok(vec) => vec,
        Err(_) => return std::ptr::null_mut(),
    };

    let mut result = MatchResult {
        is_active: false,
        course_name: String::new(),
        start_time: String::new(),
        end_time: String::new(),
        room: String::new(),
    };

    for item in items {
        if let Some(ref h) = item.hari {
            if h.trim().to_lowercase() == target_day {
                let start = item.jam_awal.as_deref().unwrap_or("");
                let end = item.jam_akhir.as_deref().unwrap_or("");
                if !start.is_empty() && !end.is_empty() {
                    if current_time >= start && current_time <= end {
                        result.is_active = true;
                        result.course_name = item
                            .matakuliah
                            .or(item.nama)
                            .unwrap_or_else(|| "Kuliah".to_string());
                        result.start_time = start.to_string();
                        result.end_time = end.to_string();
                        result.room = item.ruang.unwrap_or_default();
                        break;
                    }
                }
            }
        }
    }

    let res_json = match serde_json::to_string(&result) {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };

    match CString::new(res_json) {
        Ok(c) => c.into_raw(),
        Err(_) => std::ptr::null_mut(),
    }
}

/// In-memory cryptographic buffer obfuscator
#[no_mangle]
pub extern "C" fn konthol_crypt_buffer(
    data_ptr: *const u8,
    len: usize,
    key: u8,
) -> *mut u8 {
    if data_ptr.is_null() || len == 0 {
        return std::ptr::null_mut();
    }

    let input_slice = unsafe { std::slice::from_raw_parts(data_ptr, len) };
    let mut output = Vec::with_capacity(len);
    for (i, &b) in input_slice.iter().enumerate() {
        output.push(b ^ key ^ ((i as u8).wrapping_mul(7)));
    }

    let mut boxed_slice = output.into_boxed_slice();
    let raw_ptr = boxed_slice.as_mut_ptr();
    std::mem::forget(boxed_slice);
    raw_ptr
}

/// Free C string allocated by Rust
#[no_mangle]
pub extern "C" fn konthol_free_string(s: *mut c_char) {
    if !s.is_null() {
        unsafe {
            let _ = CString::from_raw(s);
        }
    }
}

/// Free memory buffer allocated by Rust
#[no_mangle]
pub extern "C" fn konthol_free_buffer(buf: *mut u8, len: usize) {
    if !buf.is_null() && len > 0 {
        unsafe {
            let _ = Vec::from_raw_parts(buf, len, len);
        }
    }
}
