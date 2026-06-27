<#--
  "Sign out of other sessions" checkbox, shared by the update-password and
  update-email forms. Restyled with kc-* classes; field name/id preserved.
  Default unchecked (matches base semantics — does not silently log the user
  out of other sessions).
-->
<#macro logoutOtherSessions>
    <div class="kc-row">
        <label class="kc-check-row">
            <input type="checkbox" id="logout-sessions" name="logout-sessions" value="on" class="kc-checkbox" />
            <span class="kc-check-label">${msg("logoutOtherSessions")}</span>
        </label>
    </div>
</#macro>
