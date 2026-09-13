#ifndef KONTHOL_CORE_H
#define KONTHOL_CORE_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

int konthol_parse_cas_form(const char* html_ptr, char** out_action, char** out_fields_json);
char* konthol_match_active_schedule(const char* schedules_json_ptr, const char* day_ptr, const char* time_ptr);
unsigned char* konthol_crypt_buffer(const unsigned char* data_ptr, size_t len, unsigned char key);
void konthol_free_string(char* s);
void konthol_free_buffer(unsigned char* buf, size_t len);

#ifdef __cplusplus
}
#endif

#endif // KONTHOL_CORE_H
