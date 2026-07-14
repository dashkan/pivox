<#import "template.ftl" as layout>
<@layout.registrationLayout bodyClass="oauth"; section>
    <#if section = "header">
        <#if client.attributes.logoUri??>
            <img src="${client.attributes.logoUri}"/>
        </#if>
        <#if client.name?has_content>
            ${msg("oauthGrantTitle",advancedMsg(client.name))}
        <#else>
            ${msg("oauthGrantTitle",client.clientId)}
        </#if>
    <#elseif section = "form">
        <div id="kc-oauth" class="kc-stack">
            <div class="kc-prose flex flex-col gap-2 text-start">
                <p>${msg("oauthGrantRequest")}</p>
                <#if oauth.clientScopesRequested??>
                    <ul class="mx-0 w-auto">
                        <#list oauth.clientScopesRequested as clientScope>
                            <li>
                                <#if !clientScope.dynamicScopeParameter??>
                                    ${advancedMsg(clientScope.consentScreenText)}
                                <#else>
                                    ${advancedMsg(clientScope.consentScreenText)}: <b>${clientScope.dynamicScopeParameter}</b>
                                </#if>
                            </li>
                        </#list>
                    </ul>
                </#if>
            </div>

            <#if client.attributes.policyUri?? || client.attributes.tosUri??>
                <p class="kc-hint">
                    <#if client.name?has_content>
                        ${msg("oauthGrantInformation",advancedMsg(client.name))}
                    <#else>
                        ${msg("oauthGrantInformation",client.clientId)}
                    </#if>
                    <#if client.attributes.tosUri??>
                        ${msg("oauthGrantReview")}
                        <a href="${client.attributes.tosUri}" target="_blank" class="kc-link">${msg("oauthGrantTos")}</a>
                    </#if>
                    <#if client.attributes.policyUri??>
                        ${msg("oauthGrantReview")}
                        <a href="${client.attributes.policyUri}" target="_blank" class="kc-link">${msg("oauthGrantPolicy")}</a>
                    </#if>
                </p>
            </#if>

            <form action="${url.oauthAction}" method="POST" class="kc-actions kc-actions-row">
                <input type="hidden" name="code" value="${oauth.code}">
                <button class="kc-btn kc-btn-primary" name="accept" id="kc-login" type="submit">${msg("doYes")}</button>
                <button class="kc-btn kc-btn-outline" name="cancel" id="kc-cancel" type="submit">${msg("doNo")}</button>
            </form>
        </div>
    </#if>
</@layout.registrationLayout>
