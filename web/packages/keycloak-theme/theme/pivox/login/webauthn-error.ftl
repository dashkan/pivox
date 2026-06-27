<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=true; section>
    <#if section = "header">
        ${kcSanitize(msg("webauthn-error-title"))?no_esc}
    <#elseif section = "form">

        <script type="text/javascript">
            <#outputformat "JavaScript">
            refreshPage = () => {
                document.getElementById('isSetRetry').value = 'retry';
                document.getElementById('executionValue').value = ${execution?c};
                document.getElementById('kc-error-credential-form').requestSubmit();
            }
            </#outputformat>
        </script>

        <form id="kc-error-credential-form" class="kc-stack" action="${url.loginAction}"
              method="post">
            <input type="hidden" id="executionValue" name="authenticationExecution"/>
            <input type="hidden" id="isSetRetry" name="isSetRetry"/>
        </form>

        <div class="kc-actions">
            <input onclick="refreshPage()" type="button"
                   class="kc-btn kc-btn-primary"
                   name="try-again" id="kc-try-again" value="${kcSanitize(msg("doTryAgain"))?no_esc}"
            />

            <#if isAppInitiatedAction??>
                <form action="${url.loginAction}" class="kc-actions" id="kc-webauthn-settings-form" method="post">
                    <button type="submit"
                            class="kc-btn kc-btn-outline"
                            id="cancelWebAuthnAIA" name="cancel-aia" value="true">${msg("doCancel")}
                    </button>
                </form>
            </#if>
        </div>

    </#if>
</@layout.registrationLayout>
