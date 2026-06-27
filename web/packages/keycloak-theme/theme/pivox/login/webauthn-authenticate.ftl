<#import "template.ftl" as layout>
<@layout.registrationLayout displayInfo=(realm.registrationAllowed && !registrationDisabled??); section>
    <#if section = "title">
     title
    <#elseif section = "header">
        ${msg("webauthn-login-title")}
    <#elseif section = "form">
        <div id="kc-form-webauthn" class="kc-stack">
            <form id="webauth" action="${url.loginAction}" method="post">
                <input type="hidden" id="clientDataJSON" name="clientDataJSON"/>
                <input type="hidden" id="authenticatorData" name="authenticatorData"/>
                <input type="hidden" id="signature" name="signature"/>
                <input type="hidden" id="credentialId" name="credentialId"/>
                <input type="hidden" id="userHandle" name="userHandle"/>
                <input type="hidden" id="error" name="error"/>
            </form>

            <#if authenticators??>
                <form id="authn_select" class="kc-stack">
                    <#list authenticators.authenticators as authenticator>
                        <input type="hidden" name="authn_use_chk" value="${authenticator.credentialId}"/>
                    </#list>
                </form>

                <#if shouldDisplayAuthenticators?? && shouldDisplayAuthenticators>
                    <#if authenticators.authenticators?size gt 1>
                        <p class="kc-prose">${msg("webauthn-available-authenticators")}</p>
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
                                            ${msg('webauthn-createdAt-label')}
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

            <div id="kc-form-buttons" class="kc-actions">
                <input id="authenticateWebAuthnButton" type="button" autofocus="autofocus"
                       value="${msg("webauthn-doAuthenticate")}"
                       class="kc-btn kc-btn-primary"/>
            </div>
        </div>

    <script type="module">
        <#outputformat "JavaScript">
        import { authenticateByWebAuthn } from "${url.resourcesPath}/js/webauthnAuthenticate.js";
        const authButton = document.getElementById('authenticateWebAuthnButton');
        authButton.addEventListener("click", function() {
            const input = {
                isUserIdentified : ${isUserIdentified},
                challenge : ${challenge?c},
                userVerification : ${userVerification?c},
                rpId : ${rpId?c},
                createTimeout : ${createTimeout?c},
                errmsg : ${msg("webauthn-unsupported-browser-text")?c}
            };
            authenticateByWebAuthn(input);
        }, { once: true });
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
