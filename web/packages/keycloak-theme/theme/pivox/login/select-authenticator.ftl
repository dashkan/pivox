<#import "template.ftl" as layout>
<@layout.registrationLayout displayInfo=false; section>
    <#if section = "header">
        ${msg("loginChooseAuthenticator")}
    <#elseif section = "form">
        <form id="kc-select-credential-form" class="kc-actions" action="${url.loginAction}" method="post">
            <#list auth.authenticationSelections as authenticationSelection>
                <button class="kc-select-item" type="submit" name="authenticationExecution" value="${authenticationSelection.authExecId}">
                    <div class="flex flex-col gap-0.5">
                        <span class="kc-select-item-title">${msg('${authenticationSelection.displayName}')}</span>
                        <span class="kc-hint">${msg('${authenticationSelection.helpText}')}</span>
                    </div>
                </button>
            </#list>
        </form>
    </#if>
</@layout.registrationLayout>
