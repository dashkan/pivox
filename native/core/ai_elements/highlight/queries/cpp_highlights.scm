; Conservative highlights for tree-sitter-cpp — only uses node types
; that definitely exist in the shipped grammar. Tree-sitter-highlight
; rejects the WHOLE query on a single unknown node or anonymous token,
; so we keep this deliberately minimal and expand only after verifying
; each addition works.

; ─── Literals & comments ──────────────────────────────────────────
(comment) @comment
(number_literal) @number
(string_literal) @string
(char_literal) @string

; ─── Types ────────────────────────────────────────────────────────
(primitive_type) @type.builtin
(type_identifier) @type

; ─── Functions ────────────────────────────────────────────────────
(function_declarator
  declarator: (identifier) @function)

(call_expression
  function: (identifier) @function)

; ─── Members ──────────────────────────────────────────────────────
(field_identifier) @property

; ─── Keywords (anonymous tokens) ──────────────────────────────────
"class" @keyword
"struct" @keyword
"union" @keyword
"enum" @keyword
"namespace" @keyword
"using" @keyword
"typedef" @keyword
"template" @keyword
"typename" @keyword
"public" @keyword
"private" @keyword
"protected" @keyword
"virtual" @keyword
"override" @keyword
"const" @keyword
"static" @keyword
"inline" @keyword
"extern" @keyword
"return" @keyword
"if" @keyword
"else" @keyword
"for" @keyword
"while" @keyword
"do" @keyword
"switch" @keyword
"case" @keyword
"default" @keyword
"break" @keyword
"continue" @keyword
"try" @keyword
"catch" @keyword
"throw" @keyword
"new" @keyword
"delete" @keyword
"sizeof" @keyword

; Omitted — named nodes in tree-sitter-cpp, not anonymous tokens:
;   auto, operator, override, final
; If they need coloring, match via their node types (auto as part of
; type_descriptor, operator_name under function_declarator, etc.)

; true / false / null / nullptr are named nodes in tree-sitter-cpp,
; not anonymous tokens — match by node type.
(true) @constant.builtin
(false) @constant.builtin
(null) @constant.builtin
