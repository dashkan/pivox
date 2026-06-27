<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=false; section>
    <#if section = "header">
        ${msg("deleteCredentialTitle", credentialLabel)}
    <#elseif section = "form">
    <div id="kc-delete-text" class="kc-prose">
        ${msg("deleteCredentialMessage", credentialLabel)}
    </div>
    <form class="kc-actions" action="${url.loginAction}" method="POST">
        <input class="kc-btn kc-btn-destructive" name="accept" id="kc-accept" type="submit" value="${msg("doConfirmDelete")}"/>
        <input class="kc-btn kc-btn-outline" name="cancel-aia" value="${msg("doCancel")}" id="kc-decline" type="submit" />
    </form>
    </#if>
</@layout.registrationLayout>
