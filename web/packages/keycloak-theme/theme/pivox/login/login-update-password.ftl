<#import "template.ftl" as layout>
<#import "password-commons.ftl" as passwordCommons>
<#import "commons.ftl" as commons>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('password','password-confirm'); section>
    <#if section = "header">
        ${msg("updatePasswordTitle")}
    <#elseif section = "form">
        <form id="kc-passwd-update-form" class="flex flex-col gap-4" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post">
            <@commons.passwordInput id="password-new" name="password-new" label=msg("passwordNew")
                autocomplete="new-password" autofocus=true ariaInvalid=messagesPerField.existsError('password','password-confirm') />
            <#if messagesPerField.existsError('password')>
                <div class="px-4"><span id="input-error-password" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('password'))?no_esc}</span></div>
            </#if>

            <@commons.passwordInput id="password-confirm" name="password-confirm" label=msg("passwordConfirm")
                autocomplete="new-password" ariaInvalid=messagesPerField.existsError('password-confirm') />
            <#if messagesPerField.existsError('password-confirm')>
                <div class="px-4"><span id="input-error-password-confirm" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('password-confirm'))?no_esc}</span></div>
            </#if>

            <#if !isAppInitiatedAction??>
                <div class="kc-row">
                    <label class="kc-check-row">
                        <input type="checkbox" id="logout-sessions" name="logout-sessions" value="on" class="kc-checkbox" />
                        <span class="kc-check-label">${msg("logoutOtherSessions")}</span>
                    </label>
                </div>
            </#if>

            <div class="kc-actions">
                <button name="login" class="kc-btn kc-btn-primary" type="submit">${msg("doSubmit")}</button>
                <#if isAppInitiatedAction??>
                    <button class="kc-btn kc-btn-outline" type="submit" name="cancel-aia" value="true">${msg("doCancel")}</button>
                </#if>
            </div>
        </form>
        <@commons.passwordToggleScript />
    </#if>
</@layout.registrationLayout>
