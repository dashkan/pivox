<#import "template.ftl" as layout>
<@layout.registrationLayout displayInfo=false displayMessage=!messagesPerField.existsError('username'); section>
    <#if section = "header">
        ${msg("emailForgotTitle")}
    <#elseif section = "form">
        <form id="kc-reset-password-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">
            <div class="kc-field">
                <label for="username" class="kc-label"><#if !realm.loginWithEmailAllowed>${msg("username")}<#elseif !realm.registrationEmailAsUsername>${msg("usernameOrEmail")}<#else>${msg("email")}</#if></label>
                <input type="text" id="username" name="username" class="kc-input" autofocus
                       value="${(auth.attemptedUsername!'')}" placeholder="name@example.com"
                       aria-invalid="<#if messagesPerField.existsError('username')>true</#if>" dir="ltr" />
            </div>
            <div class="kc-actions">
                <#if messagesPerField.existsError('username')>
                    <span id="input-error-username" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('username'))?no_esc}</span>
                </#if>
                <button class="kc-btn kc-btn-primary" type="submit">${msg("doSubmit")}</button>
            </div>
        </form>
    <#elseif section = "footer">
        <div class="kc-footer">
            <a href="${url.loginUrl}" class="kc-link">${kcSanitize(msg("backToLogin"))?no_esc}</a>
        </div>
    </#if>
</@layout.registrationLayout>
