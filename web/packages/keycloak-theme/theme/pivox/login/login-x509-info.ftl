<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("doLogIn")}
    <#elseif section = "form">

        <form id="kc-x509-login-info" class="flex flex-col gap-4" action="${url.loginAction}" method="post">
            <div class="kc-field">
                <label for="certificate_subjectDN" class="kc-label">${msg("clientCertificate")}</label>
                <#if x509.formData.subjectDN??>
                    <label id="certificate_subjectDN" class="kc-prose">${(x509.formData.subjectDN!"")}</label>
                <#else>
                    <label id="certificate_subjectDN" class="kc-prose">${msg("noCertificate")}</label>
                </#if>
            </div>

            <#if x509.formData.isUserEnabled??>
                <div class="kc-field">
                    <label for="username" class="kc-label">${msg("doX509Login")}</label>
                    <label id="username" class="kc-prose">${(x509.formData.username!'')}</label>
                </div>
            </#if>

            <div class="kc-actions">
                <input class="kc-btn kc-btn-primary" name="login" id="kc-login" type="submit" value="${msg("doContinue")}"/>
                <#if x509.formData.isUserEnabled??>
                    <input class="kc-btn kc-btn-outline" name="cancel" id="kc-cancel" type="submit" value="${msg("doIgnore")}"/>
                </#if>
            </div>
        </form>
    </#if>

</@layout.registrationLayout>
