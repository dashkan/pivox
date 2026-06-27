<#import "template.ftl" as layout>
<#import "user-profile-commons.ftl" as userProfileCommons>
<@layout.registrationLayout displayMessage=messagesPerField.exists('global') displayRequiredFields=true; section>
    <#if section = "header">
        ${msg("loginProfileTitle")}
    <#elseif section = "form">
        <form id="kc-update-profile-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">

            <@userProfileCommons.userProfileFormFields/>

            <div class="kc-actions">
                <#if isAppInitiatedAction??>
                    <input class="kc-btn kc-btn-primary" type="submit" value="${msg("doSubmit")}" />
                    <button class="kc-btn kc-btn-outline" type="submit" name="cancel-aia" value="true" formnovalidate>${msg("doCancel")}</button>
                <#else>
                    <input class="kc-btn kc-btn-primary" type="submit" value="${msg("doSubmit")}" />
                </#if>
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
