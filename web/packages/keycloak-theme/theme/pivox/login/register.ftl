<#import "template.ftl" as layout>
<#import "user-profile-commons.ftl" as userProfileCommons>
<#import "register-commons.ftl" as registerCommons>
<#import "commons.ftl" as commons>
<@layout.registrationLayout displayMessage=messagesPerField.exists('global') displayRequiredFields=true; section>
    <#if section = "header">
        <#if messageHeader??>${kcSanitize(msg("${messageHeader}"))?no_esc}<#else>${msg("registerTitle")}</#if>
    <#elseif section = "form">
        <form id="kc-register-form" class="flex flex-col gap-4" action="${url.registrationAction}" method="post">

            <#-- Dynamic user-profile attributes (realm-configured). Password
                 fields are injected right after the username/email field. -->
            <@userProfileCommons.userProfileFormFields; callback, attribute>
                <#if callback = "afterField">
                    <#if passwordRequired?? && (attribute.name == 'username' || (attribute.name == 'email' && realm.registrationEmailAsUsername))>
                        <@commons.passwordInput id="password" name="password" label=msg("password")
                            autocomplete="new-password" ariaInvalid=messagesPerField.existsError('password','password-confirm') />
                        <#if messagesPerField.existsError('password')>
                            <div class="px-4"><span id="input-error-password" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('password'))?no_esc}</span></div>
                        </#if>
                        <@commons.passwordInput id="password-confirm" name="password-confirm" label=msg("passwordConfirm")
                            autocomplete="new-password" ariaInvalid=messagesPerField.existsError('password-confirm') />
                        <#if messagesPerField.existsError('password-confirm')>
                            <div class="px-4"><span id="input-error-password-confirm" class="kc-field-error" aria-live="polite">${kcSanitize(messagesPerField.get('password-confirm'))?no_esc}</span></div>
                        </#if>
                    </#if>
                </#if>
            </@userProfileCommons.userProfileFormFields>

            <@registerCommons.termsAcceptance/>

            <#if recaptchaRequired?? && (recaptchaVisible!false)>
                <div class="kc-field">
                    <div class="g-recaptcha" data-size="compact" data-sitekey="${recaptchaSiteKey}" data-action="${recaptchaAction}"></div>
                </div>
            </#if>

            <div class="kc-actions">
                <#if recaptchaRequired?? && !(recaptchaVisible!false)>
                    <script>
                        function onSubmitRecaptcha(token) {
                            document.getElementById("kc-register-form").requestSubmit();
                        }
                    </script>
                    <button class="kc-btn kc-btn-primary g-recaptcha" data-sitekey="${recaptchaSiteKey}"
                            data-callback="onSubmitRecaptcha" data-action="${recaptchaAction}" type="submit">${msg("doRegister")}</button>
                <#else>
                    <button class="kc-btn kc-btn-primary" type="submit">${msg("doRegister")}</button>
                </#if>
            </div>
        </form>
        <@commons.passwordToggleScript />
    <#elseif section = "footer">
        <div class="kc-footer">
            <a href="${url.loginUrl}" class="kc-link">${kcSanitize(msg("backToLogin"))?no_esc}</a>
        </div>
    </#if>
</@layout.registrationLayout>
