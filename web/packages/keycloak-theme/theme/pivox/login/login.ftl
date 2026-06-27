<#import "template.ftl" as layout>
<#import "social-icons.ftl" as socialIcons>
<#import "commons.ftl" as commons>
<@layout.registrationLayout displayMessage=!messagesPerField.existsError('username','password') displayInfo=false; section>
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
                               aria-invalid="<#if messagesPerField.existsError('username','password')>true</#if>" dir="ltr" />
                    </div>
                </#if>

                <#-- On reauth the username field is hidden, so focus the password
                     immediately (matches the default theme's behavior). -->
                <@commons.passwordInput id="password" name="password" label=msg("password") autocomplete="current-password"
                    autofocus=usernameHidden?? ariaInvalid=messagesPerField.existsError('username','password') />

                <#if usernameHidden?? && messagesPerField.existsError('username','password')>
                    <div class="px-4">
                        <span id="input-error" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.getFirstError('username','password'))?no_esc}</span>
                    </div>
                </#if>

                <#if realm.rememberMe && !usernameHidden?? || realm.resetPasswordAllowed>
                    <div class="kc-row">
                        <#if realm.rememberMe && !usernameHidden??>
                            <label class="kc-check-row">
                                <input id="rememberMe" name="rememberMe" type="checkbox" class="kc-checkbox" <#if login.rememberMe??>checked</#if> />
                                <span class="kc-check-label">${msg("rememberMe")}</span>
                            </label>
                        <#else>
                            <span></span>
                        </#if>
                        <#if realm.resetPasswordAllowed>
                            <a href="${url.loginResetCredentialsUrl}" class="kc-muted-link text-sm">${msg("doForgotPassword")}</a>
                        </#if>
                    </div>
                </#if>

                <div class="kc-actions">
                    <#if !usernameHidden?? && messagesPerField.existsError('username','password')>
                        <span id="input-error" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.getFirstError('username','password'))?no_esc}</span>
                    </#if>
                    <input type="hidden" id="id-hidden-input" name="credentialId" <#if auth.selectedCredential?has_content>value="${auth.selectedCredential}"</#if> />
                    <button class="kc-btn kc-btn-primary" name="login" id="kc-login" type="submit">${msg("doLogIn")}</button>
                </div>
            </form>
            <@commons.passwordToggleScript />
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
    <#elseif section = "footer">
        <#if realm.password && realm.registrationAllowed && !registrationDisabled??>
            <div class="kc-footer">
                <span>${msg("noAccount")}</span>
                <a href="${url.registrationUrl}" class="kc-link">${msg("doRegister")}</a>
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
