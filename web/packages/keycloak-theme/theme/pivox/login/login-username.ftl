<#import "template.ftl" as layout>
<#import "passkeys.ftl" as passkeys>
<#import "social-icons.ftl" as socialIcons>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('username') displayInfo=(realm.password && realm.registrationAllowed && !registrationDisabled??); section>
    <#if section = "header">
        ${msg("loginAccountTitle")}
    <#elseif section = "form">
        <#if realm.password>
            <form id="kc-form-login" class="flex flex-col gap-4" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post">
                <#if !usernameHidden??>
                    <div class="kc-field">
                        <label for="username" class="kc-label"><#if !realm.loginWithEmailAllowed>${msg("username")}<#elseif !realm.registrationEmailAsUsername>${msg("usernameOrEmail")}<#else>${msg("email")}</#if></label>
                        <input id="username" class="kc-input" name="username" value="${(login.username!'')}" type="text"
                               autofocus autocomplete="${(enableWebAuthnConditionalUI?has_content)?then('username webauthn', 'username')}"
                               placeholder="name@example.com"
                               aria-invalid="<#if messagesPerField.existsError('username')>true</#if>" dir="ltr" />
                        <#if messagesPerField.existsError('username')>
                            <span id="input-error-username" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('username'))?no_esc}</span>
                        </#if>
                    </div>
                </#if>

                <#if realm.rememberMe && !usernameHidden??>
                    <div class="kc-row">
                        <label class="kc-check-row">
                            <input id="rememberMe" name="rememberMe" type="checkbox" class="kc-checkbox" <#if login.rememberMe??>checked</#if> />
                            <span class="kc-check-label">${msg("rememberMe")}</span>
                        </label>
                    </div>
                </#if>

                <div class="kc-actions">
                    <button class="kc-btn kc-btn-primary" name="login" id="kc-login" type="submit">${msg("doLogIn")}</button>
                </div>
            </form>
            <@passkeys.conditionalUIData />
        </#if>
    <#elseif section = "socialProviders">
        <#if realm.password && social?? && social.providers?has_content>
            <div class="kc-separator">
                <div class="kc-separator-track"><span class="kc-separator-rule"></span></div>
                <div class="kc-separator-label"><span>${msg("identity-provider-login-label")}</span></div>
            </div>
            <div id="kc-social-providers" class="kc-social-list">
                <#list social.providers as p>
                    <a id="social-${p.alias}" class="kc-btn kc-btn-outline" type="button" href="${p.loginUrl}">
                        <@socialIcons.icon provider=p.alias!p.providerId!"" />
                        <span>${p.displayName!}</span>
                    </a>
                </#list>
            </div>
        </#if>
    <#elseif section = "info">
        <#if realm.password && realm.registrationAllowed && !registrationDisabled??>
            <div id="kc-registration" class="kc-footer">
                <span>${msg("noAccount")}</span>
                <a href="${url.registrationUrl}" class="kc-link">${msg("doRegister")}</a>
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
