<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
        ${msg("oauth2DeviceVerificationTitle")}
    <#elseif section = "form">
        <form id="kc-user-verify-device-user-code-form" class="flex flex-col gap-4" action="${url.oauth2DeviceVerificationAction}" method="post">
            <div class="kc-field">
                <label for="device-user-code" class="kc-label">${msg("verifyOAuth2DeviceUserCode")}</label>
                <input id="device-user-code" name="device_user_code" autocomplete="off" type="text" class="kc-input" autofocus dir="ltr" />
            </div>
            <div class="kc-actions">
                <button class="kc-btn kc-btn-primary" type="submit">${msg("doSubmit")}</button>
            </div>
        </form>
    </#if>
</@layout.registrationLayout>
