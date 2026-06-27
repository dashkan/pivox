<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("confirmLinkIdpTitle")}
    <#elseif section = "form">
        <form id="kc-register-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">
            <div class="kc-actions">
                <button type="submit" class="kc-btn kc-btn-primary" name="submitAction" id="linkAccount" value="linkAccount">${msg("confirmLinkIdpContinue", idpDisplayName)}</button>
                <#if !hideReviewButton?has_content>
                    <button type="submit" class="kc-btn kc-btn-outline" name="submitAction" id="updateProfile" value="updateProfile">${msg("confirmLinkIdpReviewProfile")}</button>
                </#if>
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
