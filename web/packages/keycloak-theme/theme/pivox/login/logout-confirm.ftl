<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("logoutConfirmTitle")}
    <#elseif section = "form">
        <p class="kc-prose">${msg("logoutConfirmHeader")}</p>

        <form id="kc-logout-confirm" class="flex flex-col gap-4" action="${url.logoutConfirmAction}" onsubmit="confirmLogout.disabled = true; return true;" method="POST">
            <input type="hidden" name="session_code" value="${logoutConfirm.code}">
            <div class="kc-actions">
                <button class="kc-btn kc-btn-primary" name="confirmLogout" id="kc-logout" type="submit">${msg("doLogout")}</button>
            </div>
        </form>

        <#if !logoutConfirm.skipLink && (client.baseUrl)?has_content>
            <div class="kc-footer">
                <a href="${client.baseUrl}" class="kc-link">${msg("backToApplication")}</a>
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
