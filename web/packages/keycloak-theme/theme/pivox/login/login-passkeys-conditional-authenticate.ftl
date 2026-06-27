<#import "template.ftl" as layout>
<@layout.registrationLayout displayInfo=(realm.registrationAllowed && !registrationDisabled??); section>
    <#if section = "title">
     title
    <#elseif section = "header">
        ${msg("passkey-login-title")}
    <#elseif section = "form">
        <form id="webauth" action="${url.loginAction}" method="post">
            <input type="hidden" id="clientDataJSON" name="clientDataJSON"/>
            <input type="hidden" id="authenticatorData" name="authenticatorData"/>
            <input type="hidden" id="signature" name="signature"/>
            <input type="hidden" id="credentialId" name="credentialId"/>
            <input type="hidden" id="userHandle" name="userHandle"/>
            <input type="hidden" id="error" name="error"/>
        </form>

        <div class="kc-stack">
            <#if authenticators??>
                <form id="authn_select" class="kc-stack">
                    <#list authenticators.authenticators as authenticator>
                        <input type="hidden" name="authn_use_chk" value="${authenticator.credentialId}"/>
                    </#list>
                </form>

                <#if shouldDisplayAuthenticators?? && shouldDisplayAuthenticators>
                    <#if authenticators.authenticators?size gt 1>
                        <p class="kc-prose">${msg("passkey-available-authenticators")}</p>
                    </#if>

                    <div class="kc-actions">
                        <#list authenticators.authenticators as authenticator>
                            <div id="kc-webauthn-authenticator-item-${authenticator?index}" class="kc-select-item">
                                <div class="flex flex-col gap-0.5">
                                    <div id="kc-webauthn-authenticator-label-${authenticator?index}" class="kc-select-item-title">
                                        ${authenticator.label}
                                    </div>

                                    <#if authenticator.transports?? && authenticator.transports.displayNameProperties?has_content>
                                        <div id="kc-webauthn-authenticator-transport-${authenticator?index}" class="kc-hint">
                                            <#list authenticator.transports.displayNameProperties as nameProperty>
                                                <span>${msg(nameProperty)}</span>
                                                <#if nameProperty?has_next>
                                                    <span>, </span>
                                                </#if>
                                            </#list>
                                        </div>
                                    </#if>

                                    <div class="kc-hint">
                                        <span id="kc-webauthn-authenticator-createdlabel-${authenticator?index}">
                                            ${msg('passkey-createdAt-label')}
                                        </span>
                                        <span id="kc-webauthn-authenticator-created-${authenticator?index}">
                                            ${authenticator.createdAt}
                                        </span>
                                    </div>
                                </div>
                            </div>
                        </#list>
                    </div>
                </#if>
            </#if>

            <div id="kc-form">
                <div id="kc-form-wrapper">
                    <#if realm.password>
                        <form id="kc-form-login" class="kc-stack" onsubmit="login.disabled = true; return true;" action="${url.loginAction}" method="post" style="display:none">
                            <#if !usernameHidden??>
                                <div class="kc-field">
                                    <label for="username" class="kc-label">${msg("passkey-autofill-select")}</label>
                                    <input id="username"
                                        aria-invalid="<#if messagesPerField.existsError('username')>true</#if>"
                                        class="kc-input" name="username"
                                        value="${(login.username!'')}"
                                        autocomplete="username webauthn"
                                        type="text" autofocus autocomplete="off"
                                        dir="ltr"/>
                                    <#if messagesPerField.existsError('username')>
                                        <span id="input-error-username" class="kc-field-error" aria-live="polite">
                                            ${kcSanitize(messagesPerField.get('username'))?no_esc}
                                        </span>
                                    </#if>
                                </div>
                            </#if>
                        </form>
                    </#if>
                    <div id="kc-form-passkey-button" class="kc-actions" style="display:none">
                        <input id="authenticateWebAuthnButton" type="button" autofocus="autofocus"
                            value="${msg("passkey-doAuthenticate")}"
                            class="kc-btn kc-btn-primary"/>
                    </div>
                </div>
            </div>
        </div>

        <script type="module">
            <#outputformat "JavaScript">
            import { authenticateByWebAuthn } from "${url.resourcesPath}/js/webauthnAuthenticate.js";
            import { initAuthenticate } from "${url.resourcesPath}/js/passkeysConditionalAuth.js";

            const authButton = document.getElementById('authenticateWebAuthnButton');
            const input = {
                isUserIdentified : ${isUserIdentified},
                challenge : ${challenge?c},
                userVerification : ${userVerification?c},
                rpId : ${rpId?c},
                createTimeout : ${createTimeout?c},
                errmsg : ${msg("webauthn-unsupported-browser-text")?c}
            };
            authButton.addEventListener("click", () => {
                authenticateByWebAuthn(input);
            }, { once: true });

            const args = {
                isUserIdentified : ${isUserIdentified},
                challenge : ${challenge?c},
                userVerification : ${userVerification?c},
                rpId : ${rpId?c},
                createTimeout : ${createTimeout?c},
                errmsg : ${msg("passkey-unsupported-browser-text")?c}
            };

            document.addEventListener("DOMContentLoaded", (event) => initAuthenticate(args, (available) => {
                if (available) {
                    document.getElementById("kc-form-login").style.display = "block";
                } else {
                    document.getElementById("kc-form-passkey-button").style.display = 'block';
                }
            }));
            </#outputformat>
        </script>

    <#elseif section = "info">
        <#if realm.registrationAllowed && !registrationDisabled??>
            <div id="kc-registration" class="kc-footer">
                <span>${msg("noAccount")}</span>
                <a href="${url.registrationUrl}" class="kc-link">${msg("doRegister")}</a>
            </div>
        </#if>
    </#if>

</@layout.registrationLayout>
