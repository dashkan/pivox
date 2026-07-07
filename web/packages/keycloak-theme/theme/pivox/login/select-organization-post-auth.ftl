<#--
  Picker for the Select Organization (Post-Auth) authenticator from
  pivox-keycloak-spi (io.github.revspot.keycloak.selectorg). Styled to match our
  native `select-organization.ftl`, but driven by that authenticator's template
  attributes — `organizations` (List<OrgBean> with .name/.alias) + `username` —
  NOT the native `user.organizations`. Distinct filename so it doesn't shadow /
  isn't shadowed by Keycloak's native org-selection template.
-->
<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("organization.select")}
    <#elseif section = "form">
        <form id="kc-user-organizations" class="kc-actions" action="${url.loginAction}" method="post">
            <#list organizations as organization>
                <button type="submit" id="organization-${organization.alias}" class="kc-select-item"
                   name="kc.org" value="${organization.alias!}">
                    <span class="kc-select-item-title">${organization.name!}</span>
                </button>
            </#list>
        </form>
    </#if>

</@layout.registrationLayout>
