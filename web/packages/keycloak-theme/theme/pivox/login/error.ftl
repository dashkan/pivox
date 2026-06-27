<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=false; section>
    <#if section = "header">
        ${kcSanitize(msg("errorTitle"))?no_esc}
    <#elseif section = "form">
        <div id="kc-error-message" class="kc-prose">
            <p>${kcSanitize(message.summary)?no_esc}</p>
            <#if traceId??>
                <p id="traceId" class="kc-hint text-center">${msg("traceIdSupportMessage", traceId)}</p>
            </#if>
        </div>
        <#if !skipLink?? && client?? && client.baseUrl?has_content>
            <div class="kc-actions">
                <a id="backToApplication" class="kc-btn kc-btn-primary" href="${client.baseUrl}">${msg("backToApplication")}</a>
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
