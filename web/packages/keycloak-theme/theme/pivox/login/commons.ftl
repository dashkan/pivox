<#--
  Shared field macros for the Pivox login theme. Self-contained: the password
  reveal toggle uses our own inline script (passwordToggleScript) rather than
  Keycloak's passwordVisibility.js, so behavior + markup are fully owned.
-->

<#-- A labeled password input with a reveal (eye) toggle. -->
<#macro passwordInput id name label autocomplete="current-password" autofocus=false ariaInvalid=false>
    <div class="kc-field">
        <label for="${id}" class="kc-label">${label}</label>
        <div class="kc-input-group" dir="ltr">
            <input id="${id}" name="${name}" type="password" class="kc-input" autocomplete="${autocomplete}"
                   <#if autofocus>autofocus</#if> aria-invalid="${ariaInvalid?c}" />
            <button type="button" class="kc-input-toggle" data-pw-toggle="${id}" aria-controls="${id}"
                    aria-label="${msg('showPassword')}" tabindex="-1">
                <svg class="pw-eye" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/>
                    <circle cx="12" cy="12" r="3"/>
                </svg>
                <svg class="pw-eye-off hidden" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/>
                    <path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/>
                    <path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/>
                    <path d="m2 2 20 20"/>
                </svg>
            </button>
        </div>
    </div>
</#macro>

<#-- Segmented one-time-code input (shadcn input-otp look). Renders `length`
     slot cells over a single real input (id/name), wired by otpScript. -->
<#macro otpInput id name length=6 autocomplete="one-time-code" autofocus=false ariaInvalid=false>
    <div class="kc-otp" data-otp-length="${length}">
        <div class="kc-otp-group" aria-hidden="true">
            <#list 1..length as i><div class="kc-otp-slot"></div></#list>
        </div>
        <input id="${id}" name="${name}" type="text" inputmode="numeric" autocomplete="${autocomplete}"
               maxlength="${length}" class="kc-otp-input" dir="ltr"
               aria-invalid="${ariaInvalid?c}" <#if autofocus>autofocus</#if> />
    </div>
</#macro>

<#-- One-time script that wires every .kc-otp on the page. -->
<#macro otpScript>
    <script>
        document.querySelectorAll(".kc-otp").forEach(function (wrap) {
            var len = parseInt(wrap.getAttribute("data-otp-length") || "6", 10);
            var input = wrap.querySelector(".kc-otp-input");
            var slots = wrap.querySelectorAll(".kc-otp-slot");
            // Active cell follows the caret (clamped to the last cell), so arrow
            // keys and clicks position editing within a specific digit.
            function caretIndex() {
                var pos = input.selectionStart;
                if (pos === null) pos = input.value.length;
                return Math.min(pos, len - 1);
            }
            function render() {
                var val = input.value;
                var focused = document.activeElement === input;
                var active = caretIndex();
                slots.forEach(function (slot, i) {
                    var isActive = focused && i === active;
                    // Show the fake caret in the active cell only when it's empty
                    // (an active filled cell just gets the ring).
                    if (isActive && !val[i]) {
                        slot.innerHTML = '<span class="kc-otp-caret-bar"></span>';
                    } else {
                        slot.textContent = val[i] || "";
                    }
                    slot.classList.toggle("kc-otp-active", isActive);
                });
            }
            // Overwrite-at-caret: typing a digit sets the cell at the caret and
            // advances — so you can click any cell and retype it, even when the
            // code is full, without deleting first. (A plain input would insert.)
            input.addEventListener("keydown", function (event) {
                if (
                    event.key.length === 1 &&
                    /\d/.test(event.key) &&
                    input.selectionStart === input.selectionEnd
                ) {
                    event.preventDefault();
                    var pos = Math.min(input.selectionStart, len - 1);
                    var chars = input.value.split("");
                    chars[pos] = event.key;
                    input.value = chars.join("").slice(0, len);
                    input.setSelectionRange(pos + 1, pos + 1);
                    render();
                }
            });
            // Paste / autofill / range-replace still flow through here (filtered).
            input.addEventListener("input", function () {
                input.value = input.value.replace(/\D/g, "").slice(0, len);
                render();
            });
            // Click a cell -> place the caret there (can't go past typed length).
            input.addEventListener("click", function (event) {
                var rect = wrap.getBoundingClientRect();
                var idx = Math.floor(((event.clientX - rect.left) / rect.width) * len);
                idx = Math.max(0, Math.min(idx, input.value.length));
                input.setSelectionRange(idx, idx);
                render();
            });
            ["focus", "blur", "keyup", "select"].forEach(function (ev) {
                input.addEventListener(ev, render);
            });
            render();
        });
    </script>
</#macro>

<#-- Inline notice with a per-type icon (info / warning / error / success).
     Body is the message content (may contain markup). Reads as a tinted notice,
     not an input box. -->
<#macro alert type="info" role="status">
    <div class="kc-alert kc-alert-${type}" role="${role}">
        <svg class="kc-alert-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <#switch type>
                <#case "error"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/><#break>
                <#case "warning"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/><#break>
                <#case "success"><path d="M21.801 10A10 10 0 1 1 17 3.335"/><path d="m9 11 3 3L22 4"/><#break>
                <#default><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>
            </#switch>
        </svg>
        <div class="kc-alert-text"><#nested></div>
    </div>
</#macro>

<#-- One-time script that wires every [data-pw-toggle] button on the page. -->
<#macro passwordToggleScript>
    <script>
        document.querySelectorAll("[data-pw-toggle]").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var input = document.getElementById(btn.getAttribute("data-pw-toggle"));
                if (!input) return;
                var reveal = input.type === "password";
                input.type = reveal ? "text" : "password";
                btn.querySelector(".pw-eye").classList.toggle("hidden", reveal);
                btn.querySelector(".pw-eye-off").classList.toggle("hidden", !reveal);
            });
        });
    </script>
</#macro>
