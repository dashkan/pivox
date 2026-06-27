<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("pageExpiredTitle")}
    <#elseif section = "form">
        <p class="kc-prose">${msg("pageExpiredMsg1")}</p>
        <div class="kc-actions">
            <a id="loginRestartLink" class="kc-btn kc-btn-primary" href="${url.loginRestartFlowUrl}">${msg("doClickHere")}</a>
            <a id="loginContinueLink" class="kc-btn kc-btn-outline" href="${url.loginAction}">${msg("pageExpiredMsg2")}</a>
        </div>
    </#if>
</@layout.registrationLayout>
