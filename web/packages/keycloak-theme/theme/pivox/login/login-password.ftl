<#import "template.ftl" as layout>
<#import "commons.ftl" as commons>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('password'); section>
    <#if section = "header">
        ${msg("doLogIn")}
    <#elseif section = "form">
        <form id="kc-form-login" class="flex flex-col gap-4" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post">
            <@commons.passwordInput id="password" name="password" label=msg("password") autocomplete="current-password" autofocus=true
                ariaInvalid=messagesPerField.existsError('password') />

            <#if messagesPerField.existsError('password')>
                <div class="px-4">
                    <span id="input-error-password" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('password'))?no_esc}</span>
                </div>
            </#if>

            <#if realm.resetPasswordAllowed>
                <div class="kc-row">
                    <span></span>
                    <a href="${url.loginResetCredentialsUrl}" class="kc-muted-link text-sm">${msg("doForgotPassword")}</a>
                </div>
            </#if>

            <div class="kc-actions">
                <button class="kc-btn kc-btn-primary" name="login" id="kc-login" type="submit">${msg("doLogIn")}</button>
            </div>
        </form>
        <@commons.passwordToggleScript />
    </#if>
</@layout.registrationLayout>
