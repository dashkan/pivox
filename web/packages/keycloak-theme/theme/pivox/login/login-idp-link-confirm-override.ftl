<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("confirmOverrideIdpTitle")}
    <#elseif section = "form">
        <form id="kc-register-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">
            <div class="kc-prose">
                <p>${msg("pageExpiredMsg1")} <a id="loginRestartLink" class="kc-link" href="${url.loginRestartFlowUrl}">${msg("doClickHere")}</a></p>
            </div>
            <div class="kc-actions">
                <button type="submit" class="kc-btn kc-btn-primary" name="submitAction" id="confirmOverride" value="confirmOverride">${msg("confirmOverrideIdpContinue", idpDisplayName)}</button>
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
