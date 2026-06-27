<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('recoveryCodeInput'); section>

    <#if section = "header">
        ${msg("auth-recovery-code-header")}
    <#elseif section = "form">
        <form id="kc-recovery-code-login-form" class="flex flex-col gap-4" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post">
            <div class="kc-field">
                <label for="recoveryCodeInput" class="kc-label">${msg("auth-recovery-code-prompt", recoveryAuthnCodesInputBean.codeNumber?c)}</label>
                <input id="recoveryCodeInput"
                       name="recoveryCodeInput"
                       aria-invalid="<#if messagesPerField.existsError('recoveryCodeInput')>true</#if>"
                       autocomplete="one-time-code"
                       type="text"
                       class="kc-input"
                       inputmode="numeric"
                       autofocus
                       dir="ltr"/>

                <#if messagesPerField.existsError('recoveryCodeInput')>
                    <span id="input-error" class="kc-field-error" aria-live="polite">
                        ${kcSanitize(messagesPerField.get('recoveryCodeInput'))?no_esc}
                    </span>
                </#if>
            </div>

            <div id="kc-form-buttons" class="kc-actions">
                <input
                        class="kc-btn kc-btn-primary"
                        name="login" id="kc-login" type="submit" value="${msg("doLogIn")}" />
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
