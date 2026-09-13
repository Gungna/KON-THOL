package core_bridge

/*
#cgo CFLAGS: -I${SRCDIR}/../core/include
#cgo LDFLAGS: ${SRCDIR}/../core/target/release/libkonthol_core.a -ldl -lpthread -lm
#include <stdlib.h>

int konthol_parse_cas_form(const char* html_ptr, char** out_action, char** out_fields_json);
char* konthol_match_active_schedule(const char* schedules_json_ptr, const char* day_ptr, const char* time_ptr);
unsigned char* konthol_crypt_buffer(const unsigned char* data_ptr, size_t len, unsigned char key);
void konthol_free_string(char* s);
void konthol_free_buffer(unsigned char* buf, size_t len);
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"
)

type MatchResult struct {
	IsActive   bool   `json:"is_active"`
	CourseName string `json:"course_name"`
	StartTime  string `json:"start_time"`
	EndTime    string `json:"end_time"`
	Room       string `json:"room"`
}

// ParseCASForm calls Rust scraper to extract form action and fields with zero memory leak
func ParseCASForm(html string) (string, map[string]string, error) {
	cHTML := C.CString(html)
	defer C.free(unsafe.Pointer(cHTML))

	var cAction *C.char
	var cFields *C.char

	res := C.konthol_parse_cas_form(cHTML, &cAction, &cFields)
	if res <= 0 {
		return "", nil, fmt.Errorf("rust parser returned status %d", int(res))
	}
	defer C.konthol_free_string(cAction)
	defer C.konthol_free_string(cFields)

	action := C.GoString(cAction)
	fieldsJSON := C.GoString(cFields)

	var fields map[string]string
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		return "", nil, err
	}

	return action, fields, nil
}

// MatchActiveSchedule calls Rust deterministic schedule matcher
func MatchActiveSchedule(schedulesJSON string, day string, timeStr string) (*MatchResult, error) {
	cJSON := C.CString(schedulesJSON)
	cDay := C.CString(day)
	cTime := C.CString(timeStr)
	defer C.free(unsafe.Pointer(cJSON))
	defer C.free(unsafe.Pointer(cDay))
	defer C.free(unsafe.Pointer(cTime))

	cResult := C.konthol_match_active_schedule(cJSON, cDay, cTime)
	if cResult == nil {
		return nil, fmt.Errorf("rust schedule matcher failed")
	}
	defer C.konthol_free_string(cResult)

	resJSON := C.GoString(cResult)
	var mr MatchResult
	if err := json.Unmarshal([]byte(resJSON), &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// CryptBuffer calls Rust buffer obfuscator
func CryptBuffer(data []byte, key byte) []byte {
	if len(data) == 0 {
		return nil
	}
	cData := (*C.uchar)(unsafe.Pointer(&data[0]))
	cLen := C.size_t(len(data))
	cKey := C.uchar(key)

	cOut := C.konthol_crypt_buffer(cData, cLen, cKey)
	if cOut == nil {
		return nil
	}
	defer C.konthol_free_buffer(cOut, cLen)

	return C.GoBytes(unsafe.Pointer(cOut), C.int(len(data)))
}
