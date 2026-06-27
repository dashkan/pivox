<#import "template.ftl" as layout>
<#import "password-commons.ftl" as passwordCommons>
<#import "user-profile-commons.ftl" as userProfileCommons>
<@layout.registrationLayout displayMessage=messagesPerField.exists('global') displayRequiredFields=true; section>
    <#if section = "header">
        ${msg("updateEmailTitle")}
    <#elseif section = "form">
        <form id="kc-update-email-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">

            <@userProfileCommons.userProfileFormFields/>

            <@passwordCommons.logoutOtherSessions/>

            <div class="kc-actions">
                <#if isAppInitiatedAction??>
                    <input class="kc-btn kc-btn-primary" type="submit" value="${msg("doSubmit")}" />
                    <button class="kc-btn kc-btn-outline" type="submit" name="cancel-aia" value="true">${msg("doCancel")}</button>
                <#else>
                    <input class="kc-btn kc-btn-primary" type="submit" value="${msg("doSubmit")}" />
                </#if>
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
