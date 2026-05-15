#ifndef PIVOX_HIGHLIGHT_H
#define PIVOX_HIGHLIGHT_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct PivoxHighlighter PivoxHighlighter;

typedef struct {
    uint32_t start;
    uint32_t end;
    int32_t highlight_id;  // Index into highlight names, or -1 for plain source.
} PivoxHighlightSpan;

typedef struct {
    PivoxHighlightSpan* spans;
    uint32_t count;
} PivoxHighlightResult;

// Create/destroy a highlighter instance (owns all language configs).
PivoxHighlighter* pivox_highlighter_create(void);
void pivox_highlighter_destroy(PivoxHighlighter* highlighter);

// Highlight source code in the given language.
// Returns spans that must be freed with pivox_highlight_result_free.
PivoxHighlightResult pivox_highlight(PivoxHighlighter* highlighter,
                                      const char* language,
                                      const uint8_t* source,
                                      uint32_t source_len);

// Free a highlight result.
void pivox_highlight_result_free(PivoxHighlightResult result);

// Get the name of a highlight ID (e.g. "keyword", "string", "comment").
// Returns NULL if out of range.
const char* pivox_highlight_name(int32_t id);

// Get the total number of recognized highlight names.
uint32_t pivox_highlight_name_count(void);

#ifdef __cplusplus
}
#endif

#endif // PIVOX_HIGHLIGHT_H
