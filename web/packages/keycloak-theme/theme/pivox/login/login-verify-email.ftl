<#import "template.ftl" as layout>
<@layout.registrationLayout displayInfo=false; section>
    <#if section = "header">
        ${msg("emailVerifyTitle")}
    <#elseif section = "form">
        <p class="kc-prose">
            <#if verifyEmail??>${msg("emailVerifyInstruction1",verifyEmail)}<#else>${msg("emailVerifyInstruction4",user.email)}</#if>
        </p>
        <#if isAppInitiatedAction??>
            <form id="kc-verify-email-form" class="kc-actions kc-actions-row" action="${url.loginAction}" method="post">
                <#if verifyEmail??>
                    <button class="kc-btn kc-btn-outline" type="submit">${msg("emailVerifyResend")}</button>
                <#else>
                    <button class="kc-btn kc-btn-primary" type="submit">${msg("emailVerifySend")}</button>
                </#if>
                <button class="kc-btn kc-btn-outline" type="submit" name="cancel-aia" value="true" formnovalidate>${msg("doCancel")}</button>
            </form>
        <#else>
            <div class="kc-prose">
                ${msg("emailVerifyInstruction2")}
                <a href="${url.loginAction}" class="kc-link">${msg("doClickHere")}</a> ${msg("emailVerifyInstruction3")}
            </div>
        </#if>
    </#if>
</@layout.registrationLayout>
