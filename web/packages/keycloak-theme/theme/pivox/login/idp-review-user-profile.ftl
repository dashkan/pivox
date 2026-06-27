<#import "template.ftl" as layout>
<#import "user-profile-commons.ftl" as userProfileCommons>
<@layout.registrationLayout displayMessage=messagesPerField.exists('global') displayRequiredFields=true; section>
    <#if section = "header">
        ${msg("loginIdpReviewProfileTitle")}
    <#elseif section = "form">
        <form id="kc-idp-review-profile-form" class="flex flex-col gap-4" action="${url.loginAction}" method="post">

            <@userProfileCommons.userProfileFormFields/>

            <div class="kc-actions">
                <input class="kc-btn kc-btn-primary" type="submit" value="${msg("doSubmit")}" />
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
