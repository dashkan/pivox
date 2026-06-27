<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        <#if code.success>
            ${msg("codeSuccessTitle")}
        <#else>
            ${kcSanitize(msg("codeErrorTitle", code.error))}
        </#if>
    <#elseif section = "form">
        <div id="kc-code" class="kc-field">
            <#if code.success>
                <p class="kc-prose">${msg("copyCodeInstruction")}</p>
                <input id="code" class="kc-input" value="${code.code}"/>
            <#else>
                <p id="error" class="kc-field-error">${kcSanitize(code.error)}</p>
            </#if>
        </div>
    </#if>
</@layout.registrationLayout>
