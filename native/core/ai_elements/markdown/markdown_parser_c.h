#ifndef PIVOX_MARKDOWN_PARSER_C_H
#define PIVOX_MARKDOWN_PARSER_C_H

#include <stddef.h>

// C-ABI bridge for the markdown parser.
//
// Swift (and any other language) calls `pivox_md_parse_json` with a
// markdown string and gets back a heap-allocated JSON representation of
// the typed block list. The JSON shape is a flat array of objects, each
// tagged with a "kind" field and kind-specific payload fields.
//
// Caller owns the returned string — free it via `pivox_md_free_string`.
//
// JSON shape, by kind:
//   {"kind":"paragraph","text":"..."}
//   {"kind":"heading","level":1-6,"text":"..."}
//   {"kind":"code_block","language":"...","code":"..."}
//   {"kind":"block_quote","text":"..."}
//   {"kind":"list","ordered":true|false,"start":N,"items":[
//       {"text":"...","checked":bool,"has_checkbox":bool}, ...]}
//   {"kind":"table","headers":["..."],"rows":[["..."], ...]}
//   {"kind":"thematic_break"}
//   {"kind":"html_block","html":"..."}
//   {"kind":"image","url":"...","alt":"...","title":"..."}

#ifdef __cplusplus
extern "C" {
#endif

// Parse markdown into a JSON-encoded block list. Returns a malloc'd
// NUL-terminated string the caller must free with
// pivox_md_free_string. Returns NULL on allocation failure.
char* pivox_md_parse_json(const char* markdown);

// Apply streaming-repair to a partial markdown string (close unclosed
// fences, lists, emphasis). Returns a malloc'd NUL-terminated string
// the caller must free with pivox_md_free_string. Safe to pass the
// result to pivox_md_parse_json.
char* pivox_md_fix_incomplete(const char* markdown);

// Free a string returned by pivox_md_parse_json or
// pivox_md_fix_incomplete.
void pivox_md_free_string(char* s);

#ifdef __cplusplus
}
#endif

#endif  // PIVOX_MARKDOWN_PARSER_C_H
