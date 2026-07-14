<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('totp'); section>
    <#if section="header">
        ${msg("doLogIn")}
    <#elseif section="form">
        <form id="kc-otp-reset-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">
            <p id="kc-otp-reset-form-description" class="kc-prose">${msg("otp-reset-description")}</p>

            <div class="kc-actions">
                <#list configuredOtpCredentials.userOtpCredentials as otpCredential>
                    <label class="kc-select-item">
                        <input id="kc-otp-credential-${otpCredential?index}" type="radio" name="selectedCredentialId" class="kc-checkbox"
                               value="${otpCredential.id}" <#if otpCredential.id == configuredOtpCredentials.selectedCredentialId>checked="checked"</#if> />
                        <span class="kc-select-item-title">${otpCredential.userLabel}</span>
                    </label>
                </#list>
            </div>

            <div class="kc-actions">
                <input id="kc-otp-reset-form-submit" class="kc-btn kc-btn-primary" type="submit" value="${msg("doSubmit")}"/>
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
