<#import "template.ftl" as layout>
<#import "commons.ftl" as commons>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('totp'); section>
    <#if section = "header">
        ${msg("doLogIn")}
    <#elseif section = "form">
        <form id="kc-otp-login-form" class="flex flex-col gap-4" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post">
            <#if otpLogin.userOtpCredentials?size gt 1>
                <div class="kc-actions">
                    <#list otpLogin.userOtpCredentials as otpCredential>
                        <label class="kc-select-item">
                            <input id="kc-otp-credential-${otpCredential?index}" type="radio" name="selectedCredentialId" class="kc-checkbox"
                                   value="${otpCredential.id}" <#if otpCredential.id == otpLogin.selectedCredentialId>checked="checked"</#if> />
                            <span class="kc-select-item-title">${otpCredential.userLabel}</span>
                        </label>
                    </#list>
                </div>
            </#if>

            <div class="kc-field">
                <label for="otp" class="kc-label">${msg("loginOtpOneTime")}</label>
                <@commons.otpInput id="otp" name="otp" autofocus=true
                    ariaInvalid=messagesPerField.existsError('totp') />
            </div>

            <div class="kc-actions">
                <#if messagesPerField.existsError('totp')>
                    <span id="input-error-otp-code" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('totp'))?no_esc}</span>
                </#if>
                <button class="kc-btn kc-btn-primary" name="login" id="kc-login" type="submit">${msg("doLogIn")}</button>
            </div>
        </form>
        <@commons.otpScript />
    </#if>
</@layout.registrationLayout>
