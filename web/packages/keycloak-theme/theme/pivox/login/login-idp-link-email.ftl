<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("emailLinkIdpTitle", idpDisplayName)}
    <#elseif section = "form">
        <div class="kc-prose">
            <p id="instruction1">
                ${msg("emailLinkIdp1", idpDisplayName, brokerContext.username, realm.displayName)}
            </p>
            <p id="instruction2">
                ${msg("emailLinkIdp2")} <a class="kc-link" href="${url.loginAction}">${msg("doClickHere")}</a> ${msg("emailLinkIdp3")}
            </p>
            <p id="instruction3">
                ${msg("emailLinkIdp4")} <a class="kc-link" href="${url.loginAction}">${msg("doClickHere")}</a> ${msg("emailLinkIdp5")}
            </p>
        </div>
    </#if>
</@layout.registrationLayout>
