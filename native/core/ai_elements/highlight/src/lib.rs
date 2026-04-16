use std::ffi::{CStr, c_char, c_int, c_uint};
use std::ptr;
use std::slice;

use tree_sitter_highlight::{Highlight, HighlightConfiguration, HighlightEvent, Highlighter};

/// The highlight names we recognize, in order. The index into this array
/// is the `Highlight.0` value returned by the highlighter.
static HIGHLIGHT_NAMES: &[&str] = &[
    "attribute",
    "comment",
    "constant",
    "constant.builtin",
    "constructor",
    "embedded",
    "function",
    "function.builtin",
    "function.macro",
    "keyword",
    "number",
    "operator",
    "property",
    "punctuation",
    "punctuation.bracket",
    "punctuation.delimiter",
    "string",
    "string.special",
    "tag",
    "type",
    "type.builtin",
    "variable",
    "variable.builtin",
    "variable.parameter",
];

/// A single highlighted span: byte range + highlight index.
#[repr(C)]
pub struct PivoxHighlightSpan {
    pub start: c_uint,
    pub end: c_uint,
    pub highlight_id: c_int,
}

/// Result of highlighting: array of spans.
#[repr(C)]
pub struct PivoxHighlightResult {
    pub spans: *mut PivoxHighlightSpan,
    pub count: c_uint,
}

pub struct PivoxHighlighter {
    inner: Highlighter,
    configs: Vec<(&'static str, HighlightConfiguration)>,
}

fn make_config(
    lang: tree_sitter::Language,
    highlights: &str,
    injections: &str,
    locals: &str,
) -> Option<HighlightConfiguration> {
    let mut config =
        HighlightConfiguration::new(lang, "highlight", highlights, injections, locals).ok()?;
    config.configure(HIGHLIGHT_NAMES);
    Some(config)
}

macro_rules! register_lang {
    ($configs:expr, $name:expr, $mod:ident) => {
        if let Some(config) = make_config($mod::LANGUAGE.into(), $mod::HIGHLIGHTS_QUERY, "", "") {
            $configs.push(($name, config));
        }
    };
    ($configs:expr, $name:expr, $mod:ident, highlights = $h:expr) => {
        if let Some(config) = make_config($mod::LANGUAGE.into(), $h, "", "") {
            $configs.push(($name, config));
        }
    };
    ($configs:expr, $name:expr, $mod:ident, highlights = $h:expr, injections = $i:expr) => {
        if let Some(config) = make_config($mod::LANGUAGE.into(), $h, $i, "") {
            $configs.push(($name, config));
        }
    };
    ($configs:expr, $name:expr, $mod:ident, highlights = $h:expr, locals = $l:expr) => {
        if let Some(config) = make_config($mod::LANGUAGE.into(), $h, "", $l) {
            $configs.push(($name, config));
        }
    };
    ($configs:expr, $name:expr, $mod:ident, highlights = $h:expr, injections = $i:expr, locals = $l:expr) => {
        if let Some(config) = make_config($mod::LANGUAGE.into(), $h, $i, $l) {
            $configs.push(($name, config));
        }
    };
}

#[unsafe(no_mangle)]
pub extern "C" fn pivox_highlighter_create() -> *mut PivoxHighlighter {
    let mut configs: Vec<(&'static str, HighlightConfiguration)> = Vec::new();

    register_lang!(configs, "python", tree_sitter_python,
        highlights = tree_sitter_python::HIGHLIGHTS_QUERY);

    register_lang!(configs, "javascript", tree_sitter_javascript,
        highlights = tree_sitter_javascript::HIGHLIGHT_QUERY,
        injections = tree_sitter_javascript::INJECTIONS_QUERY,
        locals = tree_sitter_javascript::LOCALS_QUERY);

    if let Some(config) = make_config(
        tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into(),
        tree_sitter_typescript::HIGHLIGHTS_QUERY,
        "",
        tree_sitter_typescript::LOCALS_QUERY,
    ) {
        configs.push(("typescript", config));
    }

    register_lang!(configs, "rust", tree_sitter_rust,
        highlights = tree_sitter_rust::HIGHLIGHTS_QUERY,
        injections = tree_sitter_rust::INJECTIONS_QUERY);

    register_lang!(configs, "go", tree_sitter_go,
        highlights = tree_sitter_go::HIGHLIGHTS_QUERY);

    register_lang!(configs, "c", tree_sitter_c,
        highlights = tree_sitter_c::HIGHLIGHT_QUERY);

    register_lang!(configs, "cpp", tree_sitter_cpp,
        highlights = tree_sitter_cpp::HIGHLIGHT_QUERY);

    register_lang!(configs, "json", tree_sitter_json,
        highlights = tree_sitter_json::HIGHLIGHTS_QUERY);

    register_lang!(configs, "bash", tree_sitter_bash,
        highlights = tree_sitter_bash::HIGHLIGHT_QUERY);

    register_lang!(configs, "html", tree_sitter_html,
        highlights = tree_sitter_html::HIGHLIGHTS_QUERY,
        injections = tree_sitter_html::INJECTIONS_QUERY);

    register_lang!(configs, "css", tree_sitter_css,
        highlights = tree_sitter_css::HIGHLIGHTS_QUERY);

    register_lang!(configs, "swift", tree_sitter_swift,
        highlights = tree_sitter_swift::HIGHLIGHTS_QUERY);

    register_lang!(configs, "yaml", tree_sitter_yaml,
        highlights = tree_sitter_yaml::HIGHLIGHTS_QUERY);

    // toml: skipped — tree-sitter-toml pins an incompatible tree-sitter core version.

    let h = PivoxHighlighter {
        inner: Highlighter::new(),
        configs,
    };
    Box::into_raw(Box::new(h))
}

#[unsafe(no_mangle)]
pub extern "C" fn pivox_highlighter_destroy(ptr: *mut PivoxHighlighter) {
    if !ptr.is_null() {
        unsafe { drop(Box::from_raw(ptr)) };
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn pivox_highlight(
    highlighter: *mut PivoxHighlighter,
    language: *const c_char,
    source: *const u8,
    source_len: c_uint,
) -> PivoxHighlightResult {
    let empty = PivoxHighlightResult {
        spans: ptr::null_mut(),
        count: 0,
    };

    if highlighter.is_null() || language.is_null() || source.is_null() {
        return empty;
    }

    let h = unsafe { &mut *highlighter };
    let lang_str = unsafe { CStr::from_ptr(language) }.to_str().unwrap_or("");
    let src = unsafe { slice::from_raw_parts(source, source_len as usize) };

    let config_idx = h.configs.iter().position(|(name, _)| *name == lang_str);
    let config_idx = match config_idx {
        Some(i) => i,
        None => return empty,
    };

    let configs_ref = &h.configs;
    let result = h.inner.highlight(
        &configs_ref[config_idx].1,
        src,
        None,
        |injection_name| {
            configs_ref
                .iter()
                .position(|(name, _)| *name == injection_name)
                .map(|i| &configs_ref[i].1)
        },
    );

    let events = match result {
        Ok(events) => events,
        Err(_) => return empty,
    };

    let mut spans: Vec<PivoxHighlightSpan> = Vec::new();
    let mut current_highlight: c_int = -1;

    for event in events {
        match event {
            Ok(HighlightEvent::Source { start, end }) => {
                spans.push(PivoxHighlightSpan {
                    start: start as c_uint,
                    end: end as c_uint,
                    highlight_id: current_highlight,
                });
            }
            Ok(HighlightEvent::HighlightStart(Highlight(id))) => {
                current_highlight = id as c_int;
            }
            Ok(HighlightEvent::HighlightEnd) => {
                current_highlight = -1;
            }
            Err(_) => break,
        }
    }

    let count = spans.len() as c_uint;
    let ptr = if spans.is_empty() {
        ptr::null_mut()
    } else {
        let boxed = spans.into_boxed_slice();
        Box::into_raw(boxed) as *mut PivoxHighlightSpan
    };

    PivoxHighlightResult { spans: ptr, count }
}

#[unsafe(no_mangle)]
pub extern "C" fn pivox_highlight_result_free(result: PivoxHighlightResult) {
    if !result.spans.is_null() && result.count > 0 {
        unsafe {
            let _ = Box::from_raw(slice::from_raw_parts_mut(
                result.spans,
                result.count as usize,
            ));
        };
    }
}

#[unsafe(no_mangle)]
pub extern "C" fn pivox_highlight_name(id: c_int) -> *const c_char {
    if id < 0 || id as usize >= HIGHLIGHT_NAMES.len() {
        return ptr::null();
    }
    HIGHLIGHT_NAMES[id as usize].as_ptr() as *const c_char
}

#[unsafe(no_mangle)]
pub extern "C" fn pivox_highlight_name_count() -> c_uint {
    HIGHLIGHT_NAMES.len() as c_uint
}
