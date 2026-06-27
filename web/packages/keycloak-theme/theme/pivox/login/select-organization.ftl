<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("organization.select")}
    <#elseif section = "form">
        <form id="kc-user-organizations" class="kc-actions" action="${url.loginAction}" method="post">
            <#list user.organizations as organization>
                <button type="submit" id="organization-${organization.alias}" class="kc-select-item"
                   name="kc.org" value="${organization.alias!}">
                    <span class="kc-select-item-title">${organization.name!}</span>
                </button>
            </#list>
        </form>
    </#if>

</@layout.registrationLayout>
