<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=false; section>
    <#if section = "header">
        ${msg("linkIdpActionTitle", idpDisplayName)}
    <#elseif section = "form">
        <div id="kc-link-text" class="kc-prose">
            <p>${msg("linkIdpActionMessage", idpDisplayName)}</p>
        </div>
        <form class="flex flex-col gap-4" action="${url.loginAction}" method="POST">
            <div class="kc-actions kc-actions-row">
                <input class="kc-btn kc-btn-primary" name="continue" id="kc-continue" type="submit" value="${msg("doContinue")}"/>
                <input class="kc-btn kc-btn-outline" name="cancel-aia" value="${msg("doCancel")}" id="kc-cancel" type="submit" />
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
