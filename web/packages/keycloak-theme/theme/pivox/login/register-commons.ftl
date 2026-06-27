<#--
  Terms-acceptance block for the register form. Restyled with kc-* classes;
  Keycloak wiring (termsAcceptanceRequired, the termsAccepted field, error key)
  preserved from the base macro.
-->
<#macro termsAcceptance>
    <#if termsAcceptanceRequired??>
        <div class="kc-field">
            <div id="kc-registration-terms-text" class="kc-prose max-h-48 overflow-y-auto text-left">
                ${kcSanitize(msg("termsText"))?no_esc}
            </div>
            <label class="kc-check-row">
                <input type="checkbox" id="termsAccepted" name="termsAccepted" class="kc-checkbox"
                       aria-invalid="<#if messagesPerField.existsError('termsAccepted')>true</#if>" />
                <span class="kc-check-label">${msg("acceptTerms")}</span>
            </label>
            <#if messagesPerField.existsError('termsAccepted')>
                <span id="input-error-terms-accepted" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('termsAccepted'))?no_esc}</span>
            </#if>
        </div>
    </#if>
</#macro>
