<#import "template.ftl" as layout>
<@layout.registrationLayout displayMessage=false; section>
    <#if section = "header">
        ${msg("termsTitle")}
    <#elseif section = "form">
        <div id="kc-terms-text" class="kc-prose text-left">
            ${kcSanitize(msg("termsText"))?no_esc}
        </div>
        <form class="kc-actions kc-actions-row" action="${url.loginAction}" method="POST">
            <button class="kc-btn kc-btn-primary" name="accept" id="kc-accept" type="submit">${msg("doAccept")}</button>
            <button class="kc-btn kc-btn-outline" name="cancel" id="kc-decline" type="submit">${msg("doDecline")}</button>
        </form>
    </#if>
</@layout.registrationLayout>
