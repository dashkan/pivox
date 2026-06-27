<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=false; section>
    <#if section = "header">
        <#if messageHeader??>${kcSanitize(msg("${messageHeader}"))?no_esc}<#else>${message.summary}</#if>
    <#elseif section = "form">
        <div id="kc-info-message" class="kc-prose">
            <p>${message.summary}<#if requiredActions??><#list requiredActions>: <b><#items as reqActionItem>${kcSanitize(msg("requiredAction.${reqActionItem}"))?no_esc}<#sep>, </#items></b></#list><#else></#if></p>
        </div>
        <#if !skipLink?? && (pageRedirectUri?has_content || actionUri?has_content || (client.baseUrl)?has_content)>
            <div class="kc-actions">
                <#if pageRedirectUri?has_content>
                    <a class="kc-btn kc-btn-primary" href="${pageRedirectUri}">${msg("backToApplication")}</a>
                <#elseif actionUri?has_content>
                    <a class="kc-btn kc-btn-primary" href="${actionUri}">${msg("proceedWithAction")}</a>
                <#elseif (client.baseUrl)?has_content>
                    <a class="kc-btn kc-btn-primary" href="${client.baseUrl}">${msg("backToApplication")}</a>
                </#if>
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
